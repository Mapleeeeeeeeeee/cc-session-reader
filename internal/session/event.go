// Package session defines the normalized domain model used by the reader.
package session

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ansiEscapePattern matches ANSI/VT100 escape sequences: an ESC (\x1b)
// followed by a CSI sequence "[ ... <final-byte>" (covers SGR colour codes
// like "\x1b[38;2;136;136;136m" and "\x1b[1m"/"\x1b[22m"/"\x1b[39m"), or a
// single two-character escape. Content characters such as the "⛁ ⛶" box
// glyphs are not escape codes and are left untouched.
var ansiEscapePattern = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[ -/]*[@-~]|[@-Z\\-_])`)

// StripANSI removes terminal control sequences from s, leaving printable
// content intact. Used when rendering command output bodies in verbose mode.
func StripANSI(s string) string {
	return ansiEscapePattern.ReplaceAllString(s, "")
}

type EventKind string

const (
	EventUserMessage      EventKind = "user_message"
	EventAssistantMessage EventKind = "assistant_message"
	EventToolResult       EventKind = "tool_result"
	EventNoise            EventKind = "noise"
	EventCompactBoundary  EventKind = "compact_boundary"
)

// Tool names. Single source of truth for tool name literals shared across the
// summarizer, formatter, and claudecodec packages.
const (
	ToolBash            = "Bash"
	ToolRead            = "Read"
	ToolEdit            = "Edit"
	ToolWrite           = "Write"
	ToolAgent           = "Agent"
	ToolGrep            = "Grep"
	ToolGlob            = "Glob"
	ToolSkill           = "Skill"
	ToolAskUserQuestion = "AskUserQuestion"
	ToolSearch          = "ToolSearch"
)

type Event struct {
	Kind      EventKind
	Timestamp string
	RawType   string

	User      *UserMessage
	Assistant *AssistantMessage
	Tool      *ToolResult
	Noise     *NoiseEvent
}

type UserMessage struct {
	Text     string
	IsAnswer bool

	// CommandMarker is the one-line representation of a slash- or bang-command
	// invocation, e.g. "[/context]" or "[!ls -la]". Empty for plain user
	// messages and for command output. When set, formatters render the marker
	// instead of Text regardless of verbosity.
	CommandMarker string

	// IsCommandNoise marks machine-generated command output that Claude Code
	// stores as a user-role entry (<local-command-stdout>, <bash-stdout>,
	// <bash-stderr>, <local-command-caveat>). The body is dropped by default
	// and only shown under -verbose-commands.
	IsCommandNoise bool

	// IsCaveat marks the boilerplate <local-command-caveat> disclaimer. It is
	// dropped unconditionally (zero information even in verbose mode).
	IsCaveat bool

	// IsSkillInjection marks a user message that injects a SKILL.md body.
	// The SkillName field carries the extracted skill name for compact rendering.
	IsSkillInjection bool
	SkillName        string
	SkillArgs        string

	// IsTeammateMessage marks a teammate-message with harness warning boilerplate.
	IsTeammateMessage bool

	// IsCommandInjection marks a <command-message>/<command-name> XML block
	// that precedes a skill injection (distinct from the existing CommandMarker
	// which covers slash-commands detected via <command-name> at line start).
	IsCommandInjection bool

	// IsContextUsage marks a /context output block (runtime token table).
	IsContextUsage bool

	// IsSystemReminder marks a <system-reminder> harness injection.
	IsSystemReminder bool
}

type Usage struct {
	InputTokens              int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	OutputTokens             int
}

// ContextTokens returns the total context window size for this API call:
// direct input plus both cache layers.
func (u Usage) ContextTokens() int {
	return u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

func (u *Usage) Equal(other *Usage) bool {
	if u == nil || other == nil {
		return u == other
	}
	return u.InputTokens == other.InputTokens &&
		u.CacheCreationInputTokens == other.CacheCreationInputTokens &&
		u.CacheReadInputTokens == other.CacheReadInputTokens &&
		u.OutputTokens == other.OutputTokens
}

type AssistantMessage struct {
	Text     string
	Thinking []string
	ToolUses []ToolUse
	Usage    *Usage
}

type ToolUse struct {
	ID    string
	Name  string
	Input ToolInput
	Cwd   string
}

type ToolInput struct {
	Raw map[string]any
}

func (i ToolInput) String(key string) string {
	if v, ok := i.Raw[key].(string); ok {
		return v
	}
	return ""
}

func (i ToolInput) MarshalNoEscape() string {
	if i.Raw == nil {
		return "{}"
	}
	return MarshalNoEscape(i.Raw)
}

// MarshalNoEscape JSON-encodes v without HTML escaping.
// Returns "{}" for nil values or encoding errors.
func MarshalNoEscape(v any) string {
	if v == nil {
		return "{}"
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "{}"
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// ToolShortID returns the last 4 characters of a tool_use_id as a short identifier.
func ToolShortID(id string) string {
	if len(id) <= 4 {
		return id
	}
	return id[len(id)-4:]
}

// ShortID truncates id to maxLen characters.
func ShortID(id string, maxLen int) string {
	if len(id) > maxLen {
		return id[:maxLen]
	}
	return id
}

type ToolResult struct {
	ToolUseID string
	Success   bool
	Text      string
	RawName   string

	// DiffStat carries the parsed structuredPatch summary for a successful
	// Edit/Write result (ADR-003 decision 3), computed by the codec from
	// toolUseResult. Nil means no diff info is available (wrong tool, or
	// structuredPatch/content missing or unparsable) — Summary() then falls
	// back to the bare status line it always rendered.
	DiffStat *DiffStat
}

// DiffStat summarizes a successful Edit's structuredPatch hunks or a
// successful Write's new-file content.
type DiffStat struct {
	// IsNewFile distinguishes a Write result (new file, line count from
	// content) from an Edit result (hunk-based +/- counts).
	IsNewFile bool

	// Edit fields: +/- line counts summed across every hunk, NewStartLine
	// taken from the first hunk, HunkCount the total number of hunks.
	Additions    int
	Deletions    int
	NewStartLine int
	HunkCount    int

	// NewFileLines is Write's new-file line count, derived from content.
	NewFileLines int
}

func (r ToolResult) Status() string {
	if r.Success {
		return "ok"
	}
	return "FAILED"
}

const (
	// successExcerptMaxRunes bounds the one-line summary shown after a
	// successful result, where the reader mainly wants a glance, not detail.
	successExcerptMaxRunes = 80
	// failureExcerptMaxRunes is wider than the success budget (ADR-003):
	// errors are the content least safe to drop from the compact summary.
	failureExcerptMaxRunes = 200
)

// Summary renders the "-> ..." tail appended after a tool call's label.
//
// Success is the default state and carries no marker (ADR-007 decision 3):
// 1,550 of 1,604 tool calls in the measured session succeeded, so naming it
// on every line taxed every line for information the reader already assumes.
// Only FAILED is announced. A successful call still shows its excerpt or diff
// stat, introduced by the same "->" arrow so the call and its result stay
// visually separable.
func (r ToolResult) Summary() string {
	if r.Success {
		if diff := r.diffSummary(); diff != "" {
			return diff
		}
		switch r.RawName {
		case ToolRead, ToolWrite, ToolEdit, ToolAgent:
			return ""
		}
		if excerpt := firstMeaningfulSuccessLine(r.Text, successExcerptMaxRunes); excerpt != "" {
			return fmt.Sprintf(" -> %s", excerpt)
		}
		return ""
	}
	if excerpt := firstMeaningfulErrorLine(r.Text, failureExcerptMaxRunes); excerpt != "" {
		return fmt.Sprintf(" -> %s: %s", r.Status(), excerpt)
	}
	return fmt.Sprintf(" -> %s", r.Status())
}

// diffSummary renders the ADR-003 decision 3 diff/new-file annotation for a
// successful Edit/Write result. Returns "" when DiffStat wasn't computed (the
// codec couldn't find a structuredPatch/content to parse), so Summary() falls
// back to the bare status line.
func (r ToolResult) diffSummary() string {
	if r.DiffStat == nil {
		return ""
	}
	// A failed Edit/Write keeps its FAILED marker; a successful one shows the
	// diff stat alone, since the stat itself is proof the edit landed.
	status := ""
	if !r.Success {
		status = " " + r.Status()
	}
	if r.DiffStat.IsNewFile {
		return fmt.Sprintf(" ->%s (new file, %d lines)", status, r.DiffStat.NewFileLines)
	}
	summary := fmt.Sprintf(" ->%s (+%d, -%d @ L%d", status, r.DiffStat.Additions, r.DiffStat.Deletions, r.DiffStat.NewStartLine)
	if r.DiffStat.HunkCount > 1 {
		summary += fmt.Sprintf(", %d hunks", r.DiffStat.HunkCount)
	}
	return summary + ")"
}

// catLineNumberPrefix matches "cat -n" style line-number prefixes ("   12\t")
// that Read/Edit tool output prepends to every line — noise, not error content.
var catLineNumberPrefix = regexp.MustCompile(`^\s*\d+\t`)

// bareExitCodeLine matches a standalone "Exit code N" line. The code is
// already reflected in the FAILED status token, so per ADR-003 it is skipped
// when picking an excerpt in favor of the actual error line beneath it.
var bareExitCodeLine = regexp.MustCompile(`^Exit code \d+\s*$`)

// hookErrorBoilerplate matches PreToolUse/PostToolUse hook rejection preamble
// lines ("... hook error"), which name the hook stage rather than the actual
// failure — the reader wants what's beneath it, not the wrapper.
var hookErrorBoilerplate = regexp.MustCompile(`hook error`)

// isNoiseExcerptLine reports whether line is one of the known-noise shapes
// that should be skipped when picking a failure excerpt (ADR-003 decision 2).
func isNoiseExcerptLine(line string) bool {
	return catLineNumberPrefix.MatchString(line) ||
		bareExitCodeLine.MatchString(line) ||
		hookErrorBoilerplate.MatchString(line)
}

// progressLine matches a line where a tool announces what it is about to do
// rather than what it found: "Checking formatting...", "[STARTED] Backing up
// original state...", "> nccu-toolkit@1.17.0 dev". The answer is below it.
var progressLine = regexp.MustCompile(`^(?:\[[A-Z][A-Z ]*\]|>\s|(?:Checking|Running|Loading|Installing|Fetching|Building|Compiling|Starting)\b)`)

// versionBanner matches a short line whose payload is a version stamp, the
// shape a test runner prints before any result ("RUN v4.0.18").
var versionBanner = regexp.MustCompile(`^.{0,40}\bv?\d+\.\d+\.\d+\b.{0,10}$`)

// sectionHeaderMarkers are the rules a script echoes around its own headings.
// A heading is recognized only when the same marker closes the line, so a
// diff's "--- a/file.go" is not mistaken for one.
var sectionHeaderMarkers = []string{"===", "---", "***"}

// isEchoedSectionHeader reports whether line is a heading a script printed to
// label the output beneath it ("=== branch 落後/超前 staging ===",
// "--- HEAD ---") rather than output of its own.
func isEchoedSectionHeader(line string) bool {
	for _, marker := range sectionHeaderMarkers {
		if len(line) > 2*len(marker) && strings.HasPrefix(line, marker) && strings.HasSuffix(line, marker) {
			return true
		}
	}
	return false
}

// isSuccessNoiseLine reports the extra shapes skipped when picking an excerpt
// from a SUCCESSFUL result (ADR-007 decision 1). They are deliberately not
// applied to failures: a line that is banner-shaped in passing output can be
// the error itself in failing output, and ADR-004 keeps failure information.
func isSuccessNoiseLine(line string) bool {
	return isNoiseExcerptLine(line) ||
		isEchoedSectionHeader(line) ||
		progressLine.MatchString(line) ||
		versionBanner.MatchString(line)
}

// firstMeaningfulErrorLine returns the first non-noise line of text, skipping
// cat -n prefixes, bare "Exit code N" lines, and hook boilerplate so the
// excerpt surfaces the actual error instead of the noise around it. If every
// line is noise, it falls back to the first non-empty line rather than
// dropping the excerpt entirely.
func firstMeaningfulErrorLine(text string, maxRunes int) string {
	return firstMeaningfulLine(text, maxRunes, isNoiseExcerptLine)
}

// firstMeaningfulSuccessLine is firstMeaningfulErrorLine's counterpart for a
// successful result. Before ADR-007 the success path took the first non-empty
// line with no filtering at all, which surfaced `gh`'s usage banner as the
// state of a pull request and a script's own "--- HEAD ---" as the state of a
// working tree.
func firstMeaningfulSuccessLine(text string, maxRunes int) string {
	return firstMeaningfulLine(text, maxRunes, isSuccessNoiseLine)
}

// firstMeaningfulLine returns the first line of text that isNoise rejects
// nothing about, with terminal escape sequences removed. Falling back to the
// first non-empty line keeps an excerpt when every line looks like noise,
// rather than reporting a bare status for a call that did produce output.
func firstMeaningfulLine(text string, maxRunes int, isNoise func(string) bool) string {
	var firstNonEmpty string
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(StripANSI(raw))
		if line == "" {
			continue
		}
		if firstNonEmpty == "" {
			firstNonEmpty = line
		}
		if !isNoise(line) {
			return Truncate(line, maxRunes)
		}
	}
	return Truncate(firstNonEmpty, maxRunes)
}

type NoiseEvent struct {
	Text string
}

func FirstLine(s string, maxRunes int) string {
	line := strings.SplitN(strings.TrimSpace(s), "\n", 2)[0]
	return Truncate(line, maxRunes)
}

func Truncate(s string, maxRunes int) string {
	// Byte length >= rune count, so a string within maxRunes bytes is
	// guaranteed within maxRunes runes — a fast early return that avoids
	// allocating a rune slice for the common short-string case.
	if len(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}
