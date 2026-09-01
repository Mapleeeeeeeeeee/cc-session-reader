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

// CompactTeammateMessage strips the harness warning boilerplate from a
// teammate message, keeping only the teammate ID, summary, and body content.
func CompactTeammateMessage(text string) (string, bool) {
	if !strings.Contains(text, "<teammate-message") {
		return "", false
	}

	// Strip the warning boilerplate.
	const warningPrefix = "\n\nIMPORTANT: This is NOT from your user"
	if idx := strings.Index(text, warningPrefix); idx >= 0 {
		text = text[:idx]
	}

	// May contain multiple <teammate-message> blocks.
	var parts []string
	remaining := text
	for {
		openIdx := strings.Index(remaining, "<teammate-message")
		if openIdx < 0 {
			break
		}
		// Extract attributes from the opening tag.
		tagEnd := strings.Index(remaining[openIdx:], ">")
		if tagEnd < 0 {
			break
		}
		openingTag := remaining[openIdx : openIdx+tagEnd+1]
		tmID := extractXMLAttr(openingTag, "teammate_id")
		summary := extractXMLAttr(openingTag, "summary")

		// Extract body between > and </teammate-message>.
		bodyStart := openIdx + tagEnd + 1
		closeTag := "</teammate-message>"
		closeIdx := strings.Index(remaining[bodyStart:], closeTag)
		if closeIdx < 0 {
			break
		}
		body := strings.TrimSpace(remaining[bodyStart : bodyStart+closeIdx])

		// Format the compact line.
		var line string
		if isIdleNotification(body) {
			line = fmt.Sprintf("[teammate: %s] idle", tmID)
		} else if summary != "" {
			line = fmt.Sprintf("[teammate: %s %q]\n%s", tmID, summary, body)
		} else {
			line = fmt.Sprintf("[teammate: %s]\n%s", tmID, body)
		}
		parts = append(parts, line)

		remaining = remaining[bodyStart+closeIdx+len(closeTag):]
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
