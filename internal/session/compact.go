package session

import (
	"fmt"
	"strings"
)

// CompactTaskNotification strips XML boilerplate from task-notification
// messages, keeping only the summary and result content. Returns the
// compacted text and true, or ("", false) if the input is not a
// task-notification.
func CompactTaskNotification(text string) (string, bool) {
	if !strings.Contains(text, "<task-notification>") {
		return "", false
	}
	summary := extractXMLTag(text, "summary")
	result := extractXMLTag(text, "result")
	if summary == "" && result == "" {
		return "", false
	}
	var b strings.Builder
	if summary != "" {
		b.WriteString("[" + summary + "]\n")
	}
	if result != "" {
		b.WriteString(result)
	}
	return strings.TrimSpace(b.String()), true
}

// CompactStopHookGoal renders a Stop hook notice as "[goal] <condition>".
// The rest of the notice describes how the hook behaves and is identical
// every time. Returns the whole notice when no condition was extracted.
func CompactStopHookGoal(user *UserMessage) string {
	if user.GoalCondition == "" {
		return user.Text
	}
	return "[goal] " + user.GoalCondition
}

// CompactAgentsStopped renders the notice as "[agents stopped: N]". The
// notice also lists the stopped agents' prompts, but the harness has already
// truncated each to an unusable fragment.
func CompactAgentsStopped(user *UserMessage) string {
	return fmt.Sprintf("[agents stopped: %d]", user.StoppedAgentCount)
}

// CompactCompactionSummary replaces the harness framing around an injected
// conversation summary with a marker, keeping the body: the body is the
// previous conversation, which is what a reader inheriting this session needs.
func CompactCompactionSummary(text string) string {
	const marker = "[compaction summary]"
	idx := strings.Index(text, "Summary:")
	if idx < 0 {
		return marker + "\n" + strings.TrimSpace(text)
	}
	return marker + "\n" + strings.TrimSpace(text[idx:])
}

// CompactSkillInjection returns a one-line summary of a SKILL.md injection.
// seenSkills tracks which skills have appeared; repeats get a shorter form.
func CompactSkillInjection(user *UserMessage, seenSkills map[string]bool) string {
	repeat := seenSkills[user.SkillName]
	seenSkills[user.SkillName] = true
	if user.SkillArgs != "" {
		if repeat {
			return fmt.Sprintf("[skill: %s] (repeat) %s", user.SkillName, user.SkillArgs)
		}
		return fmt.Sprintf("[skill: %s] %s", user.SkillName, user.SkillArgs)
	}
	if repeat {
		return fmt.Sprintf("[skill: %s] (repeat)", user.SkillName)
	}
	return fmt.Sprintf("[skill: %s]", user.SkillName)
}

// teammateTagVariant describes one XML shape the harness has used to wrap a
// message from another Claude session. Open is left unterminated (no closing
// ">") so it matches regardless of which attributes the harness adds to the
// tag.
type teammateTagVariant struct {
	Open   string
	Close  string
	IDAttr string
}

// TeammateTagVariants enumerates every tag shape observed in transcripts.
// `<teammate-message>` is the original form; `<agent-message>` appeared later
// carrying the sender in `from` instead of `teammate_id`, without the
// detection prose ever changing. classify.go and CompactTeammateMessage both
// read this list so a third variant only needs to be added here once.
var TeammateTagVariants = []teammateTagVariant{
	{Open: "<teammate-message", Close: "</teammate-message>", IDAttr: "teammate_id"},
	{Open: "<agent-message", Close: "</agent-message>", IDAttr: "from"},
}

// HasTeammateMessageTag reports whether text contains an opening tag of any
// known teammate-message variant.
func HasTeammateMessageTag(text string) bool {
	for _, variant := range TeammateTagVariants {
		if strings.Contains(text, variant.Open) {
			return true
		}
	}
	return false
}

// nextTeammateTagMatch finds the earliest occurrence of any teammate tag
// variant's opening tag in text, returning the variant and its index, or
// (nil, -1) if none is present. Earliest-first ordering keeps blocks in
// document order when a message mixes tag variants.
func nextTeammateTagMatch(text string) (*teammateTagVariant, int) {
	var match *teammateTagVariant
	matchIdx := -1
	for i := range TeammateTagVariants {
		variant := &TeammateTagVariants[i]
		idx := strings.Index(text, variant.Open)
		if idx < 0 {
			continue
		}
		if matchIdx < 0 || idx < matchIdx {
			matchIdx = idx
			match = variant
		}
	}
	return match, matchIdx
}

// CompactTeammateMessage strips the harness warning boilerplate from a
// teammate message, keeping only the sender ID, summary, and body content.
func CompactTeammateMessage(text string) (string, bool) {
	if !HasTeammateMessageTag(text) {
		return "", false
	}

	// Strip the warning boilerplate.
	const warningPrefix = "\n\nIMPORTANT: This is NOT from your user"
	if idx := strings.Index(text, warningPrefix); idx >= 0 {
		text = text[:idx]
	}

	// May contain multiple teammate-message blocks, possibly mixing variants.
	var parts []string
	remaining := text
	for {
		variant, openIdx := nextTeammateTagMatch(remaining)
		if variant == nil {
			break
		}
		// Extract attributes from the opening tag.
		tagEnd := strings.Index(remaining[openIdx:], ">")
		if tagEnd < 0 {
			break
		}
		openingTag := remaining[openIdx : openIdx+tagEnd+1]
		id := extractXMLAttr(openingTag, variant.IDAttr)
		summary := extractXMLAttr(openingTag, "summary")

		// Extract body between the opening tag and its matching close tag.
		bodyStart := openIdx + tagEnd + 1
		closeIdx := strings.Index(remaining[bodyStart:], variant.Close)
		if closeIdx < 0 {
			break
		}
		body := strings.TrimSpace(remaining[bodyStart : bodyStart+closeIdx])

		// "[teammate]" with no ID covers the attribute-less <agent-message>
		// the harness has been observed to emit, rather than a stray colon.
		label := "teammate"
		if id != "" {
			label = "teammate: " + id
		}

		var line string
		if isIdleNotification(body) {
			line = fmt.Sprintf("[%s] idle", label)
		} else if summary != "" {
			line = fmt.Sprintf("[%s %q]\n%s", label, summary, body)
		} else {
			line = fmt.Sprintf("[%s]\n%s", label, body)
		}
		parts = append(parts, line)

		remaining = remaining[bodyStart+closeIdx+len(variant.Close):]
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n\n"), true
}

func isIdleNotification(body string) bool {
	return strings.Contains(body, `"idle_notification"`) ||
		(strings.Contains(body, `"idleReason"`) && len(body) < 300)
}

func extractXMLAttr(tag, attr string) string {
	key := attr + `="`
	idx := strings.Index(tag, key)
	if idx < 0 {
		return ""
	}
	start := idx + len(key)
	end := strings.Index(tag[start:], `"`)
	if end < 0 {
		return ""
	}
	return tag[start : start+end]
}

// CompactForkBoilerplate renders a worker-fork preamble as "[fork]", keeping
// whatever directive text follows the closing tag (if any) — the boilerplate
// itself is fixed and carries no information beyond "this is a fork".
func CompactForkBoilerplate(text string) string {
	const marker = "[fork]"
	const closeTag = "</fork-boilerplate>"
	idx := strings.Index(text, closeTag)
	if idx < 0 {
		return marker
	}
	directive := strings.TrimSpace(text[idx+len(closeTag):])
	if directive == "" {
		return marker
	}
	return marker + "\n" + directive
}

// CompactCoordinatorMessage renders a coordinator-to-subagent message as
// "[coordinator]\n<body>", stripping the fixed opening line the same way
// CompactTeammateMessage strips the teammate warning boilerplate.
func CompactCoordinatorMessage(text string) string {
	const marker = "[coordinator]"
	const openingLine = "The coordinator sent a message while you were working:"
	body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), openingLine))
	return marker + "\n" + body
}

// CompactCommandInjection extracts the command name and args from a
// <command-message>/<command-name>/<command-args> XML block into a single line.
func CompactCommandInjection(text string) (string, bool) {
	name := extractXMLTag(text, "command-name")
	args := extractXMLTag(text, "command-args")
	if name == "" {
		return "", false
	}
	name = strings.TrimSpace(name)
	args = strings.TrimSpace(args)
	if args != "" {
		return name + " " + args, true
	}
	return name, true
}

// CollectAgentToolIDs returns a set of tool_use_ids from Agent tool invocations
// in the given events. Used by formatters to identify agent results.
func CollectAgentToolIDs(events []Event) map[string]bool {
	ids := make(map[string]bool)
	for _, event := range events {
		if event.Assistant == nil {
			continue
		}
		for _, tool := range event.Assistant.ToolUses {
			if tool.Name == ToolAgent && tool.ID != "" {
				ids[tool.ID] = true
			}
		}
	}
	return ids
}

func extractXMLTag(text, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(text, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(text[start:], close)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(text[start : start+end])
}
