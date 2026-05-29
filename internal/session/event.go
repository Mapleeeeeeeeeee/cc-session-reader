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
}

type AssistantMessage struct {
	Text     string
	Thinking []string
	ToolUses []ToolUse
}

type ToolUse struct {
	ID    string
	Name  string
	Input ToolInput
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
}

func (r ToolResult) Status() string {
	if r.Success {
		return "ok"
	}
	return "FAILED"
}

func (r ToolResult) Summary() string {
	firstLine := FirstLine(r.Text, 80)
	if firstLine != "" {
		return fmt.Sprintf(" -> %s: %s", r.Status(), firstLine)
	}
	return fmt.Sprintf(" -> %s", r.Status())
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
