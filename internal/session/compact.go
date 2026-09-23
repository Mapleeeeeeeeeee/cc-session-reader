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

// StopHookFeedbackPrefix opens a Stop hook's condition-evaluation report: the
// header line that precedes the quoted condition. The single authoritative
// definition, shared by claudecodec's classifier (which anchors matching
// further, into the opening bracket of the quoted condition — see
// classify.go's stopHookFeedbackPrefix) and CompactStopHookFeedback below,
// which strips exactly this prefix.
const StopHookFeedbackPrefix = "Stop hook feedback:\n"

// CompactStopHookFeedback renders a Stop hook condition-evaluation report as
// "[goal feedback]" plus the body. Distinct from CompactStopHookGoal (the
// hook's one-time activation notice): this fires after a turn to say whether
// the hook's condition was met, and that verdict is the useful part.
func CompactStopHookFeedback(text string) string {
	const marker = "[goal feedback]"
	return marker + "\n" + strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), StopHookFeedbackPrefix))
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
		body = stripSubagentHandbackPreamble(body)

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

// harnessFrameIndent is the fixed two-space prefix the harness applies to
// every line of a framed body — including blank lines — so a line at column
// zero inside untrusted content (a workflow's computed task, a subagent's
// report) can't forge a frame boundary. Shared by the workflow frames and the
// subagent hand-back preamble, which both use this device.
const harnessFrameIndent = "  "

// dedentHarnessFrame strips harnessFrameIndent from every line of a framed
// body. Lines that don't carry the prefix (if the harness ever emits a
// shorter one) are left as-is rather than dropping characters that aren't
// there.
func dedentHarnessFrame(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, harnessFrameIndent)
	}
	return strings.Join(lines, "\n")
}

// CompactWorkflowUserRequest renders the workflow harness's relayed-request
// frame as "[workflow: user request]" plus the de-indented body — the user's
// own request, verbatim per the frame's own wording. Returns the marker
// alone if the frame's anchor phrase or a body after it is absent.
func CompactWorkflowUserRequest(text string) string {
	const marker = "[workflow: user request]"
	const anchor = "this request wins:"
	body := dedentedFrameBody(text, anchor)
	if body == "" {
		return marker
	}
	return marker + "\n" + body
}

// CompactWorkflowComputedTask renders the workflow harness's computed-task
// frame as "[workflow: computed task]" plus the de-indented body, the same
// way CompactForkBoilerplate keeps a fork's directive after its preamble.
func CompactWorkflowComputedTask(text string) string {
	const marker = "[workflow: computed task]"
	const anchor = "The computed task text follows:"
	body := dedentedFrameBody(text, anchor)
	if body == "" {
		return marker
	}
	return marker + "\n" + body
}

// dedentedFrameBody returns the de-indented text following anchor in text,
// or "" if anchor is absent or nothing meaningful follows it.
func dedentedFrameBody(text, anchor string) string {
	idx := strings.Index(text, anchor)
	if idx < 0 {
		return ""
	}
	body := strings.TrimPrefix(text[idx+len(anchor):], "\n")
	body = strings.TrimSpace(dedentHarnessFrame(body))
	return body
}

// subagentHandbackPrefix and subagentHandbackAnchor bracket the harness's
// hand-back preamble the same way the workflow frames bracket theirs: the
// preamble explains that the report is model output, not the user, and the
// body after the anchor is that report, indented per harnessFrameIndent.
const subagentHandbackPrefix = "[Subagent hand-back]"
const subagentHandbackAnchor = "The report follows:"

// stripSubagentHandbackPreamble removes the hand-back preamble from a
// teammate-message body that relays a subagent's final report, keeping only
// the de-indented report. Bodies that don't start with the preamble (an
// ordinary teammate message) are returned unchanged.
func stripSubagentHandbackPreamble(body string) string {
	if !strings.HasPrefix(body, subagentHandbackPrefix) {
		return body
	}
	if report := dedentedFrameBody(body, subagentHandbackAnchor); report != "" {
		return report
	}
	return body
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
