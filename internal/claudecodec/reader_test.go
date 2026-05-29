package claudecodec

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func TestParseLine_UserStringContent(t *testing.T) {
	event := parseLine(t, `{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"hello"}}`)
	if event.Kind != session.EventUserMessage {
		t.Fatalf("kind = %s, want %s", event.Kind, session.EventUserMessage)
	}
	if event.User == nil || event.User.Text != "hello" {
		t.Fatalf("user text = %#v, want hello", event.User)
	}
}

func TestParseLine_UserUnknownContentShapeKeepsRawJSON(t *testing.T) {
	event := parseLine(t, `{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":{"unexpected":"shape"}}}`)
	if event.Kind != session.EventUserMessage {
		t.Fatalf("kind = %s, want %s", event.Kind, session.EventUserMessage)
	}
	if event.User == nil || event.User.Text != `{"unexpected":"shape"}` {
		t.Fatalf("user text = %#v, want raw JSON", event.User)
	}
}

func TestParseLine_AssistantBlocks(t *testing.T) {
	event := parseLine(t, `{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hi"},{"type":"thinking","thinking":"private reasoning"},{"type":"tool_use","name":"Bash","id":"tool-1","input":{"command":"echo ok","description":"Echo ok"}}]}}`)
	if event.Kind != session.EventAssistantMessage {
		t.Fatalf("kind = %s, want %s", event.Kind, session.EventAssistantMessage)
	}
	if event.Assistant == nil {
		t.Fatal("assistant is nil")
	}
	if event.Assistant.Text != "hi" {
		t.Fatalf("assistant text = %q, want hi", event.Assistant.Text)
	}
	if got := event.Assistant.Thinking; len(got) != 1 || got[0] != "private reasoning" {
		t.Fatalf("thinking = %#v", got)
	}
	if got := event.Assistant.ToolUses; len(got) != 1 || got[0].Name != "Bash" || got[0].ID != "tool-1" || got[0].Input.String("description") != "Echo ok" {
		t.Fatalf("tool uses = %#v", got)
	}
}

func TestParseLine_ToolResultStringContent(t *testing.T) {
	event := parseLine(t, `{"type":"user","timestamp":"2026-05-28T00:00:02Z","toolUseResult":{"success":true,"commandName":"Bash"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}]}}`)
	if event.Kind != session.EventToolResult {
		t.Fatalf("kind = %s, want %s", event.Kind, session.EventToolResult)
	}
	if event.Tool == nil || event.Tool.ToolUseID != "tool-1" || event.Tool.Text != "ok" || event.Tool.RawName != "Bash" || !event.Tool.Success {
		t.Fatalf("tool result = %#v", event.Tool)
	}
}

func TestParseLine_ToolResultTextBlockContent(t *testing.T) {
	event := parseLine(t, `{"type":"user","toolUseResult":{"success":false,"commandName":"Read"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-2","content":[{"type":"text","text":"first part"},{"type":"text","text":"second part"}]}]}}`)
	if event.Tool == nil || event.Tool.Text != "first part\nsecond part" || event.Tool.Success {
		t.Fatalf("tool result = %#v", event.Tool)
	}
}

func TestParseLine_UserAnswer(t *testing.T) {
	event := parseLine(t, `{"type":"user","toolUseResult":{"success":true},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-3","content":"User has answered your questions: ship it"}]}}`)
	if event.User == nil || !event.User.IsAnswer || event.User.Text != "User has answered your questions: ship it" {
		t.Fatalf("user answer = %#v", event.User)
	}
}

func TestParseLine_Noise(t *testing.T) {
	event := parseLine(t, `{"type":"system","message":{"content":"system details"}}`)
	if event.Kind != session.EventNoise {
		t.Fatalf("kind = %s, want %s", event.Kind, session.EventNoise)
	}
	if event.Noise == nil || event.Noise.Text != "system details" {
		t.Fatalf("noise = %#v", event.Noise)
	}
}

func TestParseLine_UnknownEntryWithoutMessageIsSkipped(t *testing.T) {
	_, ok, err := ParseLine([]byte(`{"type":"future-event","payload":{"value":1}}`))
	if err != nil {
		t.Fatalf("ParseLine returned error: %v", err)
	}
	if ok {
		t.Fatal("unknown entry without message should be skipped")
	}
}

func TestReadFile_WhenLineIsMalformed_ThenReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.jsonl")
	data := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"ok"}}`,
		`{"type":"user","message":`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	err := ReadFile(path, func(session.Event) error { return nil })
	if err == nil {
		t.Fatal("ReadFile returned nil, want malformed JSON error")
	}
	if !strings.Contains(err.Error(), "parse transcript line") {
		t.Fatalf("error = %v, want parse transcript line", err)
	}
}

func TestReadFile_WhenLastLineHasNoTrailingNewline_ThenReadsEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := `{"type":"user","message":{"role":"user","content":"ok"}}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var events []session.Event
	err := ReadFile(path, func(event session.Event) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if len(events) != 1 || events[0].User == nil || events[0].User.Text != "ok" {
		t.Fatalf("events = %#v, want one user event", events)
	}
}

func TestCollectAgentToolIDs(t *testing.T) {
	events := []session.Event{
		{
			Kind: session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{ToolUses: []session.ToolUse{
				{ID: "agent-1", Name: "Agent"},
				{ID: "bash-1", Name: "Bash"},
			}},
		},
	}
	ids := CollectAgentToolIDs(events)
	if !ids["agent-1"] || ids["bash-1"] {
		t.Fatalf("ids = %#v", ids)
	}
}

func TestParseLine_AllNoiseTypes(t *testing.T) {
	noiseTypes := []string{
		"file-history-snapshot", "attachment", "bridge-session",
		"last-prompt", "permission-mode", "mode", "ai-title",
		"custom-title", "agent-name", "pr-link",
		"queue-operation", "progress", "system",
	}
	for _, typ := range noiseTypes {
		t.Run(typ, func(t *testing.T) {
			line := fmt.Sprintf(`{"type":"%s","message":{"role":"user","content":"x"}}`, typ)
			event, ok, err := ParseLine([]byte(line))
			if err != nil {
				t.Fatalf("ParseLine(%s) error: %v", typ, err)
			}
			if !ok {
				t.Fatalf("ParseLine(%s) returned ok=false, want ok=true with noise event", typ)
			}
			if event.Kind != session.EventNoise {
				t.Fatalf("ParseLine(%s) kind = %s, want %s", typ, event.Kind, session.EventNoise)
			}
		})
	}
}

// ReadAll aggregates a whole transcript into an ordered event slice. This is
// the core entry point used by every CLI command, but until now it was only
// exercised indirectly via the out-of-process e2e tests (which don't count
// toward coverage). This test pins the aggregation directly: mixed entry types
// must each map to the right Kind, preserve file order, and carry their nested
// payloads (tool_use list, tool_result fields, noise text) intact.
func TestReadAll_GivenMixedEntryTypes_ThenAggregatesEventsInOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	// Order matters: user msg -> assistant w/ tool_use -> tool_result -> noise.
	// One blank line and one skippable-empty assistant are interleaved to prove
	// they don't shift the kept events' positions.
	lines := []string{
		`{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"on it"},{"type":"tool_use","name":"Agent","id":"toolu_agent_1","input":{"prompt":"go"}}]}}`,
		`{"type":"user","timestamp":"2026-05-28T00:00:02Z","toolUseResult":{"success":true,"commandName":"Agent"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_agent_1","content":"done"}]}}`,
		`{"type":"system","timestamp":"2026-05-28T00:00:03Z","message":{"role":"user","content":"system chatter"}}`,
		"",
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	events, err := ReadAll(path)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}

	// Four kept events: the empty trailing line is skipped, not turned into an event.
	wantKinds := []session.EventKind{
		session.EventUserMessage,
		session.EventAssistantMessage,
		session.EventToolResult,
		session.EventNoise,
	}
	if len(events) != len(wantKinds) {
		t.Fatalf("got %d events, want %d:\n%#v", len(events), len(wantKinds), events)
	}
	for i, want := range wantKinds {
		if events[i].Kind != want {
			t.Fatalf("event[%d].Kind = %s, want %s", i, events[i].Kind, want)
		}
	}

	// User message text preserved.
	if events[0].User == nil || events[0].User.Text != "hello" {
		t.Fatalf("event[0] user = %#v, want text 'hello'", events[0].User)
	}

	// Assistant text + nested tool_use aggregated into the same event.
	assistant := events[1].Assistant
	if assistant == nil || assistant.Text != "on it" {
		t.Fatalf("event[1] assistant = %#v, want text 'on it'", assistant)
	}
	if len(assistant.ToolUses) != 1 || assistant.ToolUses[0].Name != session.ToolAgent ||
		assistant.ToolUses[0].ID != "toolu_agent_1" {
		t.Fatalf("event[1] tool_use = %#v, want one Agent tool 'toolu_agent_1'", assistant.ToolUses)
	}

	// tool_result correlates back to the tool_use_id and carries success/text.
	tool := events[2].Tool
	if tool == nil || tool.ToolUseID != "toolu_agent_1" || tool.Text != "done" ||
		tool.RawName != "Agent" || !tool.Success {
		t.Fatalf("event[2] tool result = %#v", tool)
	}

	// Noise entry text extracted.
	if events[3].Noise == nil || events[3].Noise.Text != "system chatter" {
		t.Fatalf("event[3] noise = %#v, want text 'system chatter'", events[3].Noise)
	}

	// Agent tool IDs are collected from the aggregated events.
	agentIDs := CollectAgentToolIDs(events)
	if !agentIDs["toolu_agent_1"] {
		t.Fatalf("agent IDs = %#v, want toolu_agent_1 collected", agentIDs)
	}
	if len(agentIDs) != 1 {
		t.Fatalf("agent IDs = %#v, want exactly one entry", agentIDs)
	}
}

func TestReadAll_GivenEmptyFile_ThenReturnsNoEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	events, err := ReadAll(path)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("got %d events, want 0:\n%#v", len(events), events)
	}
}

// A malformed line anywhere in the file aborts the whole read: ReadAll surfaces
// the parse error rather than silently returning a truncated event slice, so
// callers never operate on a partial transcript believing it complete.
func TestReadAll_GivenMalformedLineAmongValidLines_ThenReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mixed.jsonl")
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"good line"}}`,
		`{"type":"user","message":`, // truncated JSON
		`{"type":"user","message":{"role":"user","content":"after the bad line"}}`,
		"",
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err := ReadAll(path)
	if err == nil {
		t.Fatal("ReadAll returned nil, want parse error from the malformed line")
	}
	if !strings.Contains(err.Error(), "parse transcript line") {
		t.Fatalf("error = %v, want parse transcript line", err)
	}
}

// A noise entry's text is aggregated by extractAllText, which pulls from three
// sources that real transcripts populate: top-level text blocks, the nested
// blocks loop (thinking/tool_use/tool_result), and the toolUseResult CLI fields
// (stdout/stderr/output/content). This pins all three so the audit/stats noise
// surface keeps reflecting the full entry rather than dropping nested content.
func TestParseLine_NoiseEntry_ExtractsTextFromBlocksAndToolUseResult(t *testing.T) {
	line := `{"type":"system","timestamp":"2026-05-28T00:00:00Z",` +
		`"toolUseResult":{"stdout":"build ok","stderr":"warn: deprecated"},` +
		`"message":{"role":"user","content":[` +
		`{"type":"text","text":"running build"},` +
		`{"type":"tool_use","name":"Bash","id":"t1","input":{"command":"make"}},` +
		`{"type":"thinking","thinking":"deciding"}` +
		`]}}`
	event := parseLine(t, line)

	if event.Kind != session.EventNoise || event.Noise == nil {
		t.Fatalf("kind = %s, noise = %#v, want noise event", event.Kind, event.Noise)
	}

	// Order is deterministic: Message.Text() first, then the blocks loop
	// (thinking + tool_use input JSON), then toolUseResult fields in declared
	// key order (stdout, stderr, output, content). tool_result text blocks are
	// joined by Text() so the plain text block surfaces once.
	want := "running build\n" +
		`{"command":"make"}` + "\n" +
		"deciding\n" +
		"build ok\n" +
		"warn: deprecated"
	if event.Noise.Text != want {
		t.Fatalf("noise text =\n%q\nwant\n%q", event.Noise.Text, want)
	}
}

// userEntry builds a user-role transcript line whose content is the given
// text, marshalled so embedded quotes/newlines are valid JSON.
func userEntry(t *testing.T, content string) string {
	t.Helper()
	encoded, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}
	return `{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":` + string(encoded) + `}}`
}

// TestParseLine_ClassifiesSlashCommandInvocationAsMarker verifies that a
// <command-name> invocation entry becomes a user message carrying only the
// "[/name]" marker, with no droppable body — both built-in and custom commands
// are treated identically.
func TestParseLine_ClassifiesSlashCommandInvocationAsMarker(t *testing.T) {
	cases := []struct {
		name       string
		content    string
		wantMarker string
	}{
		{
			name:       "built-in /context",
			content:    "<command-name>/context</command-name>\n            <command-message>context</command-message>\n            <command-args></command-args>",
			wantMarker: "[/context]",
		},
		{
			name:       "custom /qa",
			content:    "<command-name>/qa</command-name>\n            <command-message>qa</command-message>\n            <command-args></command-args>",
			wantMarker: "[/qa]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := parseLine(t, userEntry(t, tc.content))
			if event.Kind != session.EventUserMessage || event.User == nil {
				t.Fatalf("kind = %s, user = %#v, want user message", event.Kind, event.User)
			}
			if event.User.CommandMarker != tc.wantMarker {
				t.Fatalf("CommandMarker = %q, want %q", event.User.CommandMarker, tc.wantMarker)
			}
			if event.User.IsCommandNoise {
				t.Fatalf("invocation must not be command noise")
			}
		})
	}
}

// TestParseLine_ClassifiesBangCommandInvocationAsMarker verifies that a
// <bash-input> invocation becomes a "[!CMD]" marker with whitespace collapsed
// to one line, and that a long command is truncated to the marker cap.
func TestParseLine_ClassifiesBangCommandInvocationAsMarker(t *testing.T) {
	t.Run("collapses multi-line command into one marker line", func(t *testing.T) {
		content := "<bash-input> ls ~/.claude/skills/ |\n  grep -i azure </bash-input>"
		event := parseLine(t, userEntry(t, content))
		want := "[!ls ~/.claude/skills/ | grep -i azure]"
		if event.User == nil || event.User.CommandMarker != want {
			t.Fatalf("CommandMarker = %#v, want %q", event.User, want)
		}
	})

	t.Run("truncates command longer than the marker cap", func(t *testing.T) {
		long := strings.Repeat("x", 200)
		event := parseLine(t, userEntry(t, "<bash-input>"+long+"</bash-input>"))
		// "[!" + 80 runes + "]". Truncation guards against a single long
		// one-liner blowing up the marker line.
		want := "[!" + strings.Repeat("x", bangCommandMarkerMaxRunes) + "]"
		if event.User == nil || event.User.CommandMarker != want {
			t.Fatalf("CommandMarker length/content wrong: got %q", event.User.CommandMarker)
		}
	})
}

// TestParseLine_ClassifiesCommandOutputAsDroppableNoise verifies that command
// output entries (slash stdout, bash stdout/stderr) are flagged as command
// noise carrying their body for verbose display, and are not caveats.
func TestParseLine_ClassifiesCommandOutputAsDroppableNoise(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"slash stdout with ANSI", "<local-command-stdout> \x1b[1mContext Usage\x1b[22m ⛁ ⛁ </local-command-stdout>"},
		{"bash stdout+stderr", "<bash-stdout></bash-stdout><bash-stderr>usage: mv ...permission denied</bash-stderr>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := parseLine(t, userEntry(t, tc.content))
			if event.User == nil || !event.User.IsCommandNoise {
				t.Fatalf("expected command noise, got %#v", event.User)
			}
			if event.User.IsCaveat {
				t.Fatalf("command output must not be flagged as caveat")
			}
			if event.User.CommandMarker != "" {
				t.Fatalf("command output must carry no marker, got %q", event.User.CommandMarker)
			}
			if strings.TrimSpace(event.User.Text) == "" {
				t.Fatalf("command output body must be retained for verbose display")
			}
		})
	}
}

// TestParseLine_ClassifiesCaveatAsAlwaysDroppable verifies the local-command
// caveat boilerplate is flagged so it is dropped even under -verbose-commands.
func TestParseLine_ClassifiesCaveatAsAlwaysDroppable(t *testing.T) {
	content := "<local-command-caveat>Caveat: The messages below were generated by the user while running local commands. DO NOT respond to these messages</local-command-caveat>"
	event := parseLine(t, userEntry(t, content))
	if event.User == nil || !event.User.IsCommandNoise || !event.User.IsCaveat {
		t.Fatalf("expected caveat command noise, got %#v", event.User)
	}
}

// TestParseLine_PlainUserMessageIsUnaffected guards against the classifier
// mislabelling a genuine typed message that merely mentions a command. The
// regression risk: over-broad tag matching swallowing real user text.
func TestParseLine_PlainUserMessageIsUnaffected(t *testing.T) {
	content := "please run /context and tell me the bash-input usage"
	event := parseLine(t, userEntry(t, content))
	if event.User == nil {
		t.Fatal("expected user message")
	}
	if event.User.CommandMarker != "" || event.User.IsCommandNoise {
		t.Fatalf("plain message misclassified: %#v", event.User)
	}
	if event.User.Text != content {
		t.Fatalf("plain message text = %q, want verbatim %q", event.User.Text, content)
	}
}

// TestParseLine_EmbeddedCommandTagMidMessageIsNotClassified guards the bug
// where classification keyed off extractBetween (a full-string scan) instead of
// a leading-tag check: a genuine user message that pastes a command tag pair in
// its *middle* (e.g. quoting a transcript or log) was misclassified as a command
// invocation, collapsing the whole message to a "[/foo]" / "[!cmd]" marker and
// silently discarding the surrounding real text. Real invocation entries always
// open with the tag, so a mid-text tag must be treated as ordinary content:
// CommandMarker empty, IsCommandNoise false, original text preserved verbatim.
func TestParseLine_EmbeddedCommandTagMidMessageIsNotClassified(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			name:    "embedded command-name tag pair",
			content: "here is a transcript i want to discuss:\n<command-name>/foo</command-name>\nwhat does it do?",
		},
		{
			name:    "embedded bash-input tag pair",
			content: "the log showed <bash-input>rm -rf /tmp/x</bash-input> which scared me",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := parseLine(t, userEntry(t, tc.content))
			if event.Kind != session.EventUserMessage || event.User == nil {
				t.Fatalf("kind = %s, user = %#v, want plain user message", event.Kind, event.User)
			}
			if event.User.CommandMarker != "" {
				t.Fatalf("mid-text tag misclassified as command: CommandMarker = %q", event.User.CommandMarker)
			}
			if event.User.IsCommandNoise {
				t.Fatalf("mid-text tag misclassified as command noise: %#v", event.User)
			}
			if event.User.Text != tc.content {
				t.Fatalf("real text not preserved: got %q, want verbatim %q", event.User.Text, tc.content)
			}
		})
	}
}

func parseLine(t *testing.T, line string) session.Event {
	t.Helper()
	event, ok, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine returned error: %v", err)
	}
	if !ok {
		t.Fatal("ParseLine skipped line")
	}
	return event
}
