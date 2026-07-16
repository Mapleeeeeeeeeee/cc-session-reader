package claudecodec

import (
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

// Command-related tags that Claude Code embeds in user-role transcript entries.
// Invocation tags carry a marker; output and caveat tags are command noise.
const (
	tagCommandNameOpen  = "<command-name>"
	tagCommandNameClose = "</command-name>"
	tagCommandArgsOpen  = "<command-args>"
	tagCommandArgsClose = "</command-args>"
	tagBashInputOpen    = "<bash-input>"
	tagBashInputClose   = "</bash-input>"
	tagLocalStdout      = "<local-command-stdout>"
	tagBashStdout       = "<bash-stdout>"
	tagBashStderr       = "<bash-stderr>"
	tagLocalCaveat      = "<local-command-caveat>"
)

// bangCommandMarkerMaxRunes caps the bang-command text rendered inside the
// "[!...]" marker so a long one-liner does not blow up the marker line.
const bangCommandMarkerMaxRunes = 80

// Harness-injected content markers used for classifying user messages that are
// not direct user input.
const (
	skillInjectionPrefix = "Base directory for this skill:"
	systemReminderOpen   = "<system-reminder>"
	teammateOpen         = "<teammate-message"
	teammateWarning      = "IMPORTANT: This is NOT from your user"
	contextUsageHeader   = "## Context Usage"
	contextUsageMarker   = "Estimated usage by category"
	commandMessageOpen   = "<command-message>"
	skillArgsPrefix      = "ARGUMENTS:"
)

// classifyCommandUserMessage inspects a user-role message body and returns a
// classified UserMessage when the body is a slash/bang command invocation or
// command output. It returns nil for ordinary typed messages so the caller
// falls back to plain user-message handling.
//
// Single source of truth: detection lives here in the parser layer so the
// formatter and stats consumers branch on domain fields, never re-match tags.
func classifyCommandUserMessage(text string) *session.UserMessage {
	trimmed := strings.TrimSpace(text)

	// Caveat is pure boilerplate — always droppable, even in verbose mode.
	if strings.HasPrefix(trimmed, tagLocalCaveat) {
		return &session.UserMessage{IsCommandNoise: true, IsCaveat: true}
	}

	// Slash-command invocation: extract "/context" -> marker "[/context]".
	// The HasPrefix gate mirrors the caveat/stdout branches: a real invocation
	// entry always opens with the tag. Gating first (a) skips the full-string
	// scan extractBetween does on every ordinary message, and (b) prevents a
	// genuine message that embeds "<command-name>...</command-name>" mid-text
	// (e.g. a pasted log) from being misclassified as a command and silently
	// stripped to a marker.
	if strings.HasPrefix(trimmed, tagCommandNameOpen) {
		if name := extractBetween(trimmed, tagCommandNameOpen, tagCommandNameClose); name != "" {
			return &session.UserMessage{CommandMarker: "[" + strings.TrimSpace(name) + "]"}
		}
	}

	// Bang-command invocation: extract the command -> marker "[!CMD]".
	if strings.HasPrefix(trimmed, tagBashInputOpen) {
		if cmd := extractBetween(trimmed, tagBashInputOpen, tagBashInputClose); strings.TrimSpace(cmd) != "" {
			oneLine := collapseWhitespace(cmd)
			return &session.UserMessage{CommandMarker: "[!" + session.Truncate(oneLine, bangCommandMarkerMaxRunes) + "]"}
		}
	}

	// Command output (slash stdout, bash stdout/stderr): droppable body,
	// surfaced only under -verbose-commands with ANSI stripped at render time.
	if strings.HasPrefix(trimmed, tagLocalStdout) ||
		strings.HasPrefix(trimmed, tagBashStdout) ||
		strings.HasPrefix(trimmed, tagBashStderr) {
		return &session.UserMessage{IsCommandNoise: true, Text: trimmed}
	}

	return nil
}

// classifyHarnessUserMessage detects harness-injected user messages that are
// not direct user input: skill injections, system reminders, teammate messages,
// context usage blocks, and command injection XML. Returns nil for plain
// user-typed messages so the caller falls back to normal handling.
func classifyHarnessUserMessage(text string) *session.UserMessage {
	trimmed := strings.TrimSpace(text)

	// system-reminder: strip entirely.
	if strings.HasPrefix(trimmed, systemReminderOpen) {
		return &session.UserMessage{IsSystemReminder: true}
	}

	// Skill injection: "Base directory for this skill: /path/to/skill"
	if strings.HasPrefix(trimmed, skillInjectionPrefix) {
		name := extractSkillName(trimmed)
		args := extractSkillArgs(trimmed)
		return &session.UserMessage{
			Text:             text,
			IsSkillInjection: true,
			SkillName:        name,
			SkillArgs:        args,
		}
	}

	// Teammate message with harness warning.
	if strings.Contains(trimmed, teammateOpen) && strings.Contains(trimmed, teammateWarning) {
		return &session.UserMessage{Text: text, IsTeammateMessage: true}
	}
	// Teammate message without the full warning (edge case: only the XML).
	if strings.HasPrefix(trimmed, "Another Claude session sent a message:") && strings.Contains(trimmed, teammateOpen) {
		return &session.UserMessage{Text: text, IsTeammateMessage: true}
	}

	// Context usage block (from /context command output).
	if strings.Contains(trimmed, contextUsageHeader) && strings.Contains(trimmed, contextUsageMarker) {
		return &session.UserMessage{IsContextUsage: true}
	}

	// Command injection XML: <command-message>...<command-name>/foo</command-name>
	// This fires for the XML wrapper message that precedes a skill SKILL.md
	// injection. Distinct from the <command-name>-prefixed case already handled
	// by classifyCommandUserMessage (which covers slash-command invocations that
	// start with the tag).
	if strings.HasPrefix(trimmed, commandMessageOpen) {
		return &session.UserMessage{Text: text, IsCommandInjection: true}
	}

	return nil
}

func extractSkillName(text string) string {
	// "Base directory for this skill: /Users/maple/.claude/skills/cc-session"
	// → "cc-session"
	prefix := skillInjectionPrefix
	idx := strings.Index(text, prefix)
	if idx < 0 {
		return "unknown"
	}
	pathStart := idx + len(prefix)
	rest := strings.TrimSpace(text[pathStart:])
	// Path ends at newline.
	if nl := strings.Index(rest, "\n"); nl >= 0 {
		rest = rest[:nl]
	}
	rest = strings.TrimRight(rest, "/")
	if slash := strings.LastIndex(rest, "/"); slash >= 0 {
		return rest[slash+1:]
	}
	return rest
}

func extractSkillArgs(text string) string {
	idx := strings.Index(text, skillArgsPrefix)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(text[idx+len(skillArgsPrefix):])
	// Take only the first line of args for the compact form.
	if nl := strings.Index(rest, "\n"); nl >= 0 {
		firstLine := rest[:nl]
		if len(rest) > nl+1 {
			return session.Truncate(firstLine, 120) + "..."
		}
		return session.Truncate(firstLine, 120)
	}
	return session.Truncate(rest, 120)
}

// extractBetween returns the substring between the first openTag and the next
// closeTag, or "" if either tag is absent.
func extractBetween(s, openTag, closeTag string) string {
	start := strings.Index(s, openTag)
	if start < 0 {
		return ""
	}
	start += len(openTag)
	end := strings.Index(s[start:], closeTag)
	if end < 0 {
		return ""
	}
	return s[start : start+end]
}

// collapseWhitespace folds runs of whitespace (including newlines) into single
// spaces so a multi-line bang command renders as one marker line.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
