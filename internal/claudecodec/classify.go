package claudecodec

import (
	"regexp"
	"strconv"
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
	tagLocalStderr      = "<local-command-stderr>"
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
	contextUsageHeader   = "## Context Usage"
	contextUsageMarker   = "Estimated usage by category"
	commandMessageOpen   = "<command-message>"
	skillArgsPrefix      = "ARGUMENTS:"

	taskNotificationOpen   = "<task-notification>"
	compactionSummary      = "This session is being continued from a previous conversation"
	interruptedPrefix      = "[Request interrupted by user"
	stopHookPrefix         = "A session-scoped Stop hook is now active with condition:"
	skillReloadedMarker    = "was loaded earlier"
	coordinatorMessageOpen = "The coordinator sent a message while you were working:"

	// continuePromptText is the exact harness-injected body that resumes an
	// invocation already started elsewhere. It carries no sourceToolUseID
	// link the way a skill injection does — only isMeta at the top level —
	// so it is matched on exact text rather than a prefix, the same way the
	// stop-hook goal is matched by its fixed wording.
	continuePromptText = "Continue from where you left off."

	forkBoilerplateOpen  = "<fork-boilerplate>"
	forkBoilerplateClose = "</fork-boilerplate>"

	// noVisibleOutputNudge is the exact harness nudge sent when an assistant
	// turn produced no visible output.
	noVisibleOutputNudge = "[Your previous response had no visible output. Please continue and produce a user-visible response.]"

	// midTurnOpeningLine and midTurnExplanationMarker bracket the human text
	// in a mid-turn message notice: the harness wraps a message the user sent
	// while the agent was still working in an explanation of when it arrives,
	// but the body between them is exactly what the user typed.
	midTurnOpeningLine       = "The user sent a new message while you were working:"
	midTurnExplanationMarker = "This is how Claude Code surfaces messages the user sends mid-turn"
)

var agentsStoppedCount = regexp.MustCompile(`^(\d+) background agents? (?:was|were) stopped`)

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

	// Command output (slash stdout/stderr, bash stdout/stderr): droppable
	// body, surfaced only under -verbose-commands with ANSI stripped at
	// render time.
	if strings.HasPrefix(trimmed, tagLocalStdout) ||
		strings.HasPrefix(trimmed, tagLocalStderr) ||
		strings.HasPrefix(trimmed, tagBashStdout) ||
		strings.HasPrefix(trimmed, tagBashStderr) {
		return &session.UserMessage{IsCommandNoise: true, Text: trimmed}
	}

	return nil
}

// classifySkillInjectionByLink detects a skill-body injection via the
// isMeta/sourceToolUseID link Claude Code actually writes, rather than the
// "Base directory for this skill:" line classifyHarnessUserMessage's
// text-prefix path looks for. Bundled skills (e.g. artifact-design) inject
// their body starting directly with prose and carry no such line, so the
// text-prefix path misses them; this path resolves the injection against the
// preceding Skill tool_use instead. toolCalls is nil when called from the
// stateless public ParseLine, under which this path is skipped and the
// caller falls back to the text-prefix path. Returns nil when isMeta is
// unset, sourceToolUseID doesn't resolve to a Skill tool_use, or the tool
// call carried no skill name — isMeta alone is not a skill marker, since
// image placeholders and stop-hook feedback also carry it.
func classifySkillInjectionByLink(text string, isMeta bool, sourceToolUseID string, toolCalls map[string]toolCallInfo) *session.UserMessage {
	if !isMeta || sourceToolUseID == "" || toolCalls == nil {
		return nil
	}
	call, ok := toolCalls[sourceToolUseID]
	if !ok || call.Name != session.ToolSkill || call.Skill == "" {
		return nil
	}
	return &session.UserMessage{
		Text:             text,
		IsSkillInjection: true,
		SkillName:        call.Skill,
		SkillArgs:        formatSkillArgsPreview(call.SkillArgs),
	}
}

// classifyContinuePrompt detects the exact-text isMeta continuation prompt
// that resumes an invocation already started elsewhere. isMeta alone is not
// a marker (image placeholders and stop-hook feedback also carry it, see
// classifySkillInjectionByLink), so this also requires the exact text.
func classifyContinuePrompt(text string, isMeta bool) *session.UserMessage {
	if !isMeta || strings.TrimSpace(text) != continuePromptText {
		return nil
	}
	return &session.UserMessage{Text: text, IsContinuePrompt: true}
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

	// Mid-turn user message: the body is genuinely what the user typed, sent
	// while the agent was still working on the previous turn, so it falls
	// back to plain user-message handling for everything except the
	// wrapper text stripped here.
	if strings.HasPrefix(trimmed, midTurnOpeningLine) {
		if body, ok := extractMidTurnUserText(trimmed); ok {
			return &session.UserMessage{Text: text, IsMidTurnUserMessage: true, MidTurnUserText: body}
		}
	}

	// Teammate message: detected by the XML tag alone, not by the surrounding
	// prose ("Another Claude session sent a message:", the disclaimer). Both
	// have already been reworded once without the tag changing, and the
	// harness has also been observed to skip the opening line and disclaimer
	// entirely. session.TeammateTagVariants covers every tag shape seen so
	// far; a rewording only breaks detection if it changes the tag itself.
	if session.HasTeammateMessageTag(trimmed) {
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

	// Background-task report. Recognized here rather than only at render time
	// so stats sees a domain field like every other subtype (ADR-008).
	// Matched by Contains, not HasPrefix: a CLI build wraps the tag in a
	// "[SYSTEM NOTIFICATION - NOT USER INPUT]" disclaimer that precedes it,
	// the same drift the teammate tag detection already accounts for.
	if strings.Contains(trimmed, taskNotificationOpen) {
		return &session.UserMessage{Text: text, IsTaskNotification: true}
	}

	// Coordinator-initiated round of work in a subagent transcript: the
	// coordinator's own message starts a turn the same way a teammate
	// message does, so it gets the same treatment.
	if strings.HasPrefix(trimmed, coordinatorMessageOpen) {
		return &session.UserMessage{Text: text, IsCoordinatorMessage: true}
	}

	// Worker-fork preamble: the harness's instructions for a forked worker,
	// followed by the actual directive text (if any) after the closing tag.
	if strings.HasPrefix(trimmed, forkBoilerplateOpen) {
		return &session.UserMessage{Text: text, IsForkBoilerplate: true}
	}

	// Fixed nudge sent when the previous assistant turn had no visible
	// output. Exact text, so no extraction needed.
	if trimmed == noVisibleOutputNudge {
		return &session.UserMessage{Text: text, IsNoVisibleOutputNudge: true}
	}

	// Conversation summary injected when a session continues past a
	// compaction. The body is the previous conversation, so it is kept.
	if strings.HasPrefix(trimmed, compactionSummary) {
		return &session.UserMessage{Text: text, IsCompactionSummary: true}
	}

	if strings.HasPrefix(trimmed, interruptedPrefix) {
		return &session.UserMessage{Text: text, IsInterrupted: true}
	}

	if match := agentsStoppedCount.FindStringSubmatch(trimmed); match != nil {
		count, err := strconv.Atoi(match[1])
		if err != nil {
			return nil
		}
		return &session.UserMessage{Text: text, IsAgentsStopped: true, StoppedAgentCount: count}
	}

	if strings.HasPrefix(trimmed, stopHookPrefix) {
		return &session.UserMessage{
			Text:           text,
			IsStopHookGoal: true,
			GoalCondition:  extractGoalCondition(trimmed),
		}
	}

	// Re-invocation notice for a skill whose body was injected earlier. It
	// carries no instructions of its own beyond naming the skill, so it is
	// classified as a skill injection and picks up the existing "(repeat)"
	// rendering.
	if name, ok := reloadedSkillName(trimmed); ok {
		return &session.UserMessage{Text: text, IsSkillInjection: true, SkillName: name}
	}

	return nil
}

// extractGoalCondition pulls the goal out of a Stop hook notice. The rest of
// the notice describes how the hook behaves and is identical every time, so
// the condition is the only part worth keeping. Returns "" when the quoted
// condition is absent, which leaves the compact form to fall back to the
// whole notice rather than claim a goal that isn't there.
func extractGoalCondition(text string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(text, stopHookPrefix))
	if !strings.HasPrefix(rest, `"`) {
		return ""
	}
	rest = rest[1:]
	end := strings.LastIndex(rest, `"`)
	if end <= 0 {
		return ""
	}
	return rest[:end]
}

// extractMidTurnUserText pulls the human-typed body out of a mid-turn user
// message notice, stripping the opening line and the trailing explanation
// paragraph. Returns ("", false) if the explanation marker is absent, so the
// caller does not classify a message whose shape it can't fully account for.
func extractMidTurnUserText(text string) (string, bool) {
	rest := strings.TrimPrefix(text, midTurnOpeningLine)
	explIdx := strings.Index(rest, midTurnExplanationMarker)
	if explIdx < 0 {
		return "", false
	}
	return strings.TrimSpace(rest[:explIdx]), true
}

// reloadedSkillName extracts the skill from a "Skill /foo was loaded earlier"
// notice, or reports false when text is not one.
func reloadedSkillName(text string) (string, bool) {
	const prefix = "Skill /"
	if !strings.HasPrefix(text, prefix) {
		return "", false
	}
	rest := text[len(prefix):]
	end := strings.IndexAny(rest, " \n")
	if end <= 0 {
		return "", false
	}
	name := rest[:end]
	if !strings.Contains(rest[end:], skillReloadedMarker) {
		return "", false
	}
	return name, true
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
	return formatSkillArgsPreview(strings.TrimSpace(text[idx+len(skillArgsPrefix):]))
}

// skillArgsPreviewMaxRunes caps a skill's args at one line for the compact
// "[skill: name] args" rendering.
const skillArgsPreviewMaxRunes = 120

// formatSkillArgsPreview truncates raw skill args to a single line, shared by
// the text-prefix path's "ARGUMENTS: ..." line and the Skill tool_use's
// "args" input — both carry the same value observed in real transcripts, so
// one truncation rule keeps their rendering identical regardless of which
// path classified the injection.
func formatSkillArgsPreview(raw string) string {
	if nl := strings.Index(raw, "\n"); nl >= 0 {
		firstLine := raw[:nl]
		if len(raw) > nl+1 {
			return session.Truncate(firstLine, skillArgsPreviewMaxRunes) + "..."
		}
		return session.Truncate(firstLine, skillArgsPreviewMaxRunes)
	}
	return session.Truncate(raw, skillArgsPreviewMaxRunes)
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
