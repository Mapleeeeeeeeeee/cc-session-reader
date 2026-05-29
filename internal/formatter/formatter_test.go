package formatter

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func TestFormatRead_WhenTranscriptHasDialogueAndToolUse_ThenWritesReadableTimeline(t *testing.T) {
	transcriptPath, _ := writeFormatterFixture(t)

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}

	got := out.String()

	// Tool summaries must include short IDs (last 4 chars of tool_use_id) for expand lookups.
	// Fixture tool ID is "tool-1" -> short ID "ol-1".
	if !strings.Contains(got, "[Bash#ol-1] Echo ok") {
		t.Fatalf("FormatRead output missing short ID tag in tool summary\ngot:\n%q", got)
	}
	if !strings.Contains(got, "[05-28 00:00] user:\nhello") {
		t.Fatalf("FormatRead output missing user message\ngot:\n%q", got)
	}
	if !strings.Contains(got, "[05-28 00:00] assistant:\nhi") {
		t.Fatalf("FormatRead output missing assistant message\ngot:\n%q", got)
	}
}

func TestFormatContext_WhenSessionMetadataExists_ThenWritesCompactContextWithHeader(t *testing.T) {
	transcriptPath, metaDir := writeFormatterFixture(t)

	var out bytes.Buffer
	store := parser.Store{SessionMetaDir: metaDir}
	if err := FormatContextWithStore(transcriptPath, formatterFixtureSessionID, FormatOptions{}, &out, store); err != nil {
		t.Fatalf("FormatContext returned error: %v", err)
	}

	got := out.String()

	// Context format must also include short IDs in tool summaries.
	if !strings.Contains(got, "# Session 12345678 | proj | 3m") {
		t.Fatalf("FormatContext output missing header\ngot:\n%q", got)
	}
	if !strings.Contains(got, "[Bash#ol-1] Echo ok") {
		t.Fatalf("FormatContext output missing short ID tag in tool summary\ngot:\n%q", got)
	}
	if !strings.Contains(got, "U: hello") || !strings.Contains(got, "U: next") {
		t.Fatalf("FormatContext output missing user messages\ngot:\n%q", got)
	}
}

func TestFormatRead_WhenMaxLinesReached_ThenStopsWithTruncationMessage(t *testing.T) {
	transcriptPath, _ := writeFormatterFixture(t)

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 3, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}

	want := `[05-28 00:00] user:
hello


--- truncated at 3 output lines ---
`
	if got := out.String(); got != want {
		t.Fatalf("FormatRead truncated output mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatRead_WhenVerboseAgents_ThenWritesFullAgentResult(t *testing.T) {
	transcriptPath, _ := writeAgentFormatterFixture(t)

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, FormatOptions{VerboseAgents: true}, &out); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}

	got := out.String()

	// Agent tool summaries must also include short IDs. Fixture ID "agent-tool-1" -> "ol-1".
	if !strings.Contains(got, "[Agent(general)#ol-1] Inspect project") {
		t.Fatalf("FormatRead verbose agent output missing short ID tag\ngot:\n%q", got)
	}
	if !strings.Contains(got, "agent line 1\nagent line 2") {
		t.Fatalf("FormatRead verbose agent output missing agent result text\ngot:\n%q", got)
	}
}

func TestFormatRead_WhenVerboseThinkingDisabled_ThenOmitsThinkingBlocks(t *testing.T) {
	// Default behavior (VerboseThinking: false) must reproduce the token-reduced
	// output exactly: no thinking content, regardless of what reasoning the
	// assistant message carried. Guards against thinking leaking into the default
	// read output.
	transcriptPath := writeThinkingFormatterFixture(t)

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "thinking:") {
		t.Fatalf("default read output must not contain a thinking header\ngot:\n%q", got)
	}
	if strings.Contains(got, thinkingFixtureFirstBlock) || strings.Contains(got, thinkingFixtureSecondBlock) {
		t.Fatalf("default read output must not contain thinking text\ngot:\n%q", got)
	}
	// The surrounding assistant text must still render so we know the fixture
	// itself is non-empty and the absence above is meaningful.
	if !strings.Contains(got, "[05-28 00:00] assistant:\nfinal answer") {
		t.Fatalf("read output missing assistant text\ngot:\n%q", got)
	}
}

func TestFormatRead_WhenVerboseThinkingEnabled_ThenRendersEachThinkingBlock(t *testing.T) {
	// With VerboseThinking on, every thinking block (the field is []string and
	// may hold multiple) must appear under a "thinking:" header before the
	// assistant text, in timeline order. Mutation guard: if the render loop is
	// dropped or skips blocks, these exact-string assertions go red.
	transcriptPath := writeThinkingFormatterFixture(t)

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, FormatOptions{VerboseThinking: true}, &out); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "[05-28 00:00] thinking:\n"+thinkingFixtureFirstBlock) {
		t.Fatalf("verbose-thinking read output missing first thinking block\ngot:\n%q", got)
	}
	if !strings.Contains(got, "[05-28 00:00] thinking:\n"+thinkingFixtureSecondBlock) {
		t.Fatalf("verbose-thinking read output missing second thinking block\ngot:\n%q", got)
	}
	// Thinking precedes the assistant text in the output (timeline order).
	if strings.Index(got, thinkingFixtureFirstBlock) > strings.Index(got, "assistant:\nfinal answer") {
		t.Fatalf("thinking should be rendered before assistant text\ngot:\n%q", got)
	}
}

func TestFormatContextEvents_WhenVerboseThinkingEnabled_ThenRendersThinkingWithCompactPrefix(t *testing.T) {
	// Context mode uses compact prefixes (U:/A:); thinking renders as "T:".
	// Default off => no thinking; on => each block present.
	events := []session.Event{
		{
			Kind: session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{
				Text:     "final answer",
				Thinking: []string{thinkingFixtureFirstBlock, thinkingFixtureSecondBlock},
			},
		},
	}

	var offOut bytes.Buffer
	FormatContextEvents(events, nil, FormatOptions{}, &offOut)
	if strings.Contains(offOut.String(), thinkingFixtureFirstBlock) {
		t.Fatalf("default context output must not contain thinking text\ngot:\n%q", offOut.String())
	}

	var onOut bytes.Buffer
	FormatContextEvents(events, nil, FormatOptions{VerboseThinking: true}, &onOut)
	got := onOut.String()
	if !strings.Contains(got, "T: "+thinkingFixtureFirstBlock) {
		t.Fatalf("verbose-thinking context output missing first thinking block\ngot:\n%q", got)
	}
	if !strings.Contains(got, "T: "+thinkingFixtureSecondBlock) {
		t.Fatalf("verbose-thinking context output missing second thinking block\ngot:\n%q", got)
	}
}

// commandNoiseEvents returns a fixture mirroring a real /context invocation:
// the slash marker, its ANSI-laden stdout body, a caveat, and a following
// genuine user message. Reused by read and context command-noise tests.
func commandNoiseEvents() []session.Event {
	return []session.Event{
		{Kind: session.EventUserMessage, User: &session.UserMessage{CommandMarker: "[/context]"}},
		{Kind: session.EventUserMessage, User: &session.UserMessage{
			IsCommandNoise: true,
			Text:           "\x1b[1mContext Usage\x1b[22m\n\x1b[38;2;136;136;136m⛁ ⛁ \x1b[39m claude-opus · 30k/200k",
		}},
		{Kind: session.EventUserMessage, User: &session.UserMessage{
			IsCommandNoise: true, IsCaveat: true,
			Text: "Caveat: The messages below were generated by the user while running local commands. DO NOT respond to these messages",
		}},
		{Kind: session.EventUserMessage, User: &session.UserMessage{Text: "real typed question"}},
	}
}

const commandStdoutContentMarker = "claude-opus · 30k/200k"

// TestFormatReadEvents_DefaultDropsCommandBodyKeepsMarker asserts the default
// read output shows the "[/context]" marker but contains neither the stdout
// body, ANSI escapes, nor the caveat — while the real user message survives.
func TestFormatReadEvents_DefaultDropsCommandBodyKeepsMarker(t *testing.T) {
	var out bytes.Buffer
	if err := FormatReadEvents(commandNoiseEvents(), nil, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents error: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, "[/context]") {
		t.Fatalf("default read output missing marker [/context]\ngot:\n%s", got)
	}
	if strings.Contains(got, commandStdoutContentMarker) {
		t.Fatalf("default read output must drop command stdout body\ngot:\n%s", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("default read output must not contain ANSI escapes\ngot:\n%q", got)
	}
	if strings.Contains(got, "DO NOT respond") {
		t.Fatalf("default read output must drop the caveat\ngot:\n%s", got)
	}
	if !strings.Contains(got, "real typed question") {
		t.Fatalf("genuine user message must be preserved\ngot:\n%s", got)
	}
}

// TestFormatReadEvents_VerboseCommandsShowsAnsiStrippedBody asserts that under
// -verbose-commands the stdout body appears with ANSI escapes stripped, while
// the caveat remains dropped (zero information even in verbose mode).
func TestFormatReadEvents_VerboseCommandsShowsAnsiStrippedBody(t *testing.T) {
	var out bytes.Buffer
	if err := FormatReadEvents(commandNoiseEvents(), nil, 0, FormatOptions{VerboseCommands: true}, &out); err != nil {
		t.Fatalf("FormatReadEvents error: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, commandStdoutContentMarker) {
		t.Fatalf("verbose-commands read output must show command body\ngot:\n%s", got)
	}
	if !strings.Contains(got, "Context Usage") {
		t.Fatalf("verbose-commands body must retain content text\ngot:\n%s", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("verbose-commands body must be ANSI-stripped\ngot:\n%q", got)
	}
	// "⛁" is a content glyph, not an escape code, and must survive.
	if !strings.Contains(got, "⛁") {
		t.Fatalf("verbose-commands body must keep content glyphs\ngot:\n%s", got)
	}
	if strings.Contains(got, "DO NOT respond") {
		t.Fatalf("caveat must stay dropped even under -verbose-commands\ngot:\n%s", got)
	}
}

// TestFormatContextEvents_DefaultDropsCommandBodyKeepsMarker mirrors the read
// assertions for the compact context output ("U:" prefixes).
func TestFormatContextEvents_DefaultDropsCommandBodyKeepsMarker(t *testing.T) {
	var out bytes.Buffer
	if err := FormatContextEvents(commandNoiseEvents(), nil, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatContextEvents error: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, "[/context]") {
		t.Fatalf("default context output missing marker\ngot:\n%s", got)
	}
	if strings.Contains(got, commandStdoutContentMarker) || strings.Contains(got, "DO NOT respond") {
		t.Fatalf("default context output must drop command body and caveat\ngot:\n%s", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("default context output must not contain ANSI escapes\ngot:\n%q", got)
	}
	if !strings.Contains(got, "real typed question") {
		t.Fatalf("genuine user message must be preserved\ngot:\n%s", got)
	}
}

// TestFormatContextEvents_VerboseCommandsShowsAnsiStrippedBody asserts the
// context verbose path surfaces the ANSI-stripped body and still drops caveats.
func TestFormatContextEvents_VerboseCommandsShowsAnsiStrippedBody(t *testing.T) {
	var out bytes.Buffer
	if err := FormatContextEvents(commandNoiseEvents(), nil, FormatOptions{VerboseCommands: true}, &out); err != nil {
		t.Fatalf("FormatContextEvents error: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, commandStdoutContentMarker) {
		t.Fatalf("verbose-commands context output must show command body\ngot:\n%s", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("verbose-commands context body must be ANSI-stripped\ngot:\n%q", got)
	}
	if strings.Contains(got, "DO NOT respond") {
		t.Fatalf("caveat must stay dropped even under -verbose-commands\ngot:\n%s", got)
	}
}

// TestFormatReadEvents_BangCommandMarkerRenderedDefault asserts a bang-command
// marker is shown by default while its stderr body is dropped.
func TestFormatReadEvents_BangCommandMarkerRenderedDefault(t *testing.T) {
	events := []session.Event{
		{Kind: session.EventUserMessage, User: &session.UserMessage{CommandMarker: "[!ls ~/.claude/skills/ | grep azure]"}},
		{Kind: session.EventUserMessage, User: &session.UserMessage{
			IsCommandNoise: true,
			Text:           "usage: mv [-f | -i | -n] ...permission denied",
		}},
	}
	var out bytes.Buffer
	if err := FormatReadEvents(events, nil, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "[!ls ~/.claude/skills/ | grep azure]") {
		t.Fatalf("bang marker missing\ngot:\n%s", got)
	}
	if strings.Contains(got, "permission denied") {
		t.Fatalf("bang stderr body must be dropped by default\ngot:\n%s", got)
	}
}

const (
	thinkingFixtureFirstBlock  = "weighing option A versus option B"
	thinkingFixtureSecondBlock = "option B wins because it avoids the lock"
)

// writeThinkingFormatterFixture writes a transcript whose assistant message
// carries two thinking blocks plus visible text, mirroring how Claude Code
// records reasoning before an answer.
func writeThinkingFormatterFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	transcriptPath := filepath.Join(root, formatterFixtureSessionID+".jsonl")
	transcript := `{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"question"}}
{"type":"assistant","timestamp":"2026-05-28T00:00:00Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"` + thinkingFixtureFirstBlock + `"},{"type":"thinking","thinking":"` + thinkingFixtureSecondBlock + `"},{"type":"text","text":"final answer"}]}}
`
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return transcriptPath
}

func TestFormatRead_WhenToolResultHasNoPendingTool_ThenStillWritesSummary(t *testing.T) {
	root := t.TempDir()
	transcriptPath := filepath.Join(root, formatterFixtureSessionID+".jsonl")
	transcript := `{"type":"user","timestamp":"2026-05-28T00:00:02Z","toolUseResult":{"success":true,"commandName":"Bash"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"missing-tool","content":"orphan output"}]}}
`
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}

	want := "  [Bash] -> ok: orphan output\n\n"
	if got := out.String(); got != want {
		t.Fatalf("FormatRead orphan output mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestInjectShortID(t *testing.T) {
	tests := []struct {
		name    string
		summary string
		shortID string
		want    string
	}{
		{
			name:    "given bracketed summary then inserts id before first bracket",
			summary: "[Bash] Run tests",
			shortID: "uCVa",
			want:    "[Bash#uCVa] Run tests",
		},
		{
			name:    "given parenthesized name then inserts before closing bracket",
			summary: "[Agent(general)] Inspect",
			shortID: "uCVa",
			want:    "[Agent(general)#uCVa] Inspect",
		},
		{
			// Empty short ID (tool_use with no id): summary is returned unchanged,
			// never "[Bash#] ..." with a dangling separator.
			name:    "given empty short id then returns summary unchanged",
			summary: "[Bash] Run tests",
			shortID: "",
			want:    "[Bash] Run tests",
		},
		{
			// No closing bracket to anchor on: summary is returned unchanged
			// rather than appending the id somewhere arbitrary.
			name:    "given summary without closing bracket then returns summary unchanged",
			summary: "no brackets here",
			shortID: "uCVa",
			want:    "no brackets here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := injectShortID(tt.summary, tt.shortID); got != tt.want {
				t.Fatalf("injectShortID(%q, %q) = %q, want %q", tt.summary, tt.shortID, got, tt.want)
			}
		})
	}
}

const formatterFixtureSessionID = "12345678-1234-1234-1234-123456789abc"

func writeFormatterFixture(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	transcriptPath := filepath.Join(root, formatterFixtureSessionID+".jsonl")
	transcript := `{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"hello"}}
{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hi"},{"type":"tool_use","name":"Bash","id":"tool-1","input":{"command":"echo ok","description":"Echo ok"}}]}}
{"type":"user","timestamp":"2026-05-28T00:00:02Z","toolUseResult":{"success":true,"commandName":"Bash"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}]}}
{"type":"user","timestamp":"2026-05-28T00:00:03Z","message":{"role":"user","content":"next"}}
`
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}
	meta := `{"session_id":"` + formatterFixtureSessionID + `","project_path":"/tmp/proj","duration_minutes":3}`
	if err := os.WriteFile(filepath.Join(metaDir, formatterFixtureSessionID+".json"), []byte(meta), 0o644); err != nil {
		t.Fatalf("write session meta: %v", err)
	}

	return transcriptPath, metaDir
}

func writeAgentFormatterFixture(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	transcriptPath := filepath.Join(root, formatterFixtureSessionID+".jsonl")
	transcript := `{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"delegating"},{"type":"tool_use","name":"Agent","id":"agent-tool-1","input":{"description":"Inspect project","subagent_type":"general"}}]}}
{"type":"user","timestamp":"2026-05-28T00:00:02Z","toolUseResult":{"success":true,"agentType":"general"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"agent-tool-1","content":"agent line 1\nagent line 2"}]}}
`
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return transcriptPath, root
}

func TestFormatReadEvents_WhenVerboseBash_ThenShowsFullBashOutput(t *testing.T) {
	events := []session.Event{
		{
			Kind:      session.EventAssistantMessage,
			Timestamp: "2025-05-28T00:00:00Z",
			Assistant: &session.AssistantMessage{
				ToolUses: []session.ToolUse{
					{ID: "tool-1", Name: "Bash", Input: session.ToolInput{Raw: map[string]any{"description": "Run tests"}}},
				},
			},
		},
		{
			Kind: session.EventToolResult,
			Tool: &session.ToolResult{ToolUseID: "tool-1", Success: false, Text: "FAIL line1\ndetail line2\ndetail line3"},
		},
	}
	var out bytes.Buffer
	FormatReadEvents(events, nil, 0, FormatOptions{VerboseBash: true}, &out)
	got := out.String()

	if !strings.Contains(got, "detail line3") {
		t.Fatalf("verbose bash should show full output, got:\n%s", got)
	}
	if !strings.Contains(got, "FAIL line1") {
		t.Fatalf("verbose bash should show first line of output, got:\n%s", got)
	}
	if !strings.Contains(got, "-> FAILED:") {
		t.Fatalf("verbose bash should show failure status, got:\n%s", got)
	}
}

func TestFormatReadEvents_WhenVerboseBash_ThenNonBashToolsStillCompressed(t *testing.T) {
	events := []session.Event{
		{
			Kind:      session.EventAssistantMessage,
			Timestamp: "2025-05-28T00:00:00Z",
			Assistant: &session.AssistantMessage{
				ToolUses: []session.ToolUse{
					{ID: "tool-1", Name: "Read", Input: session.ToolInput{Raw: map[string]any{"file_path": "/tmp/foo.go"}}},
				},
			},
		},
		{
			Kind: session.EventToolResult,
			Tool: &session.ToolResult{ToolUseID: "tool-1", Success: true, Text: "line1\nline2\nline3\nline4"},
		},
	}
	var out bytes.Buffer
	FormatReadEvents(events, nil, 0, FormatOptions{VerboseBash: true}, &out)
	got := out.String()

	// Non-Bash tools should be compressed to one-line summary even with verbose-bash on
	if strings.Contains(got, "line4") {
		t.Fatalf("non-Bash tool should remain compressed with verbose-bash, got:\n%s", got)
	}
	if !strings.Contains(got, "line1") {
		t.Fatalf("non-Bash tool summary should contain first line, got:\n%s", got)
	}
}

func TestFormatReadEvents_WhenToolResultIsUserAnswer_ThenWritesAnswerBlock(t *testing.T) {
	// An AskUserQuestion answer arrives as a tool_result event carrying a
	// User payload with IsAnswer=true. In the read timeline this must render as
	// a "user (answer)" block, not as a tool result summary. This pins the
	// answer branch of handleToolResultRead (the read-mode equivalent of the
	// context-mode "U (answer):" rendering).
	events := []session.Event{
		{
			Kind:      session.EventToolResult,
			Timestamp: "2026-05-28T00:00:00Z",
			User:      &session.UserMessage{Text: "ship it", IsAnswer: true},
			Tool:      &session.ToolResult{ToolUseID: "tool-1", Success: true, Text: "ship it"},
		},
	}
	var out bytes.Buffer
	if err := FormatReadEvents(events, nil, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}

	want := "[05-28 00:00] user (answer):\nship it\n\n"
	if got := out.String(); got != want {
		t.Fatalf("answer block mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatContextEvents_WhenVerboseBash_ThenShowsFullBashOutput(t *testing.T) {
	events := []session.Event{
		{
			Kind: session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{
				Text: "running",
				ToolUses: []session.ToolUse{
					{ID: "tool-1", Name: "Bash", Input: session.ToolInput{Raw: map[string]any{"description": "Check status"}}},
				},
			},
		},
		{
			Kind: session.EventToolResult,
			Tool: &session.ToolResult{ToolUseID: "tool-1", Success: true, Text: "ok line1\nok line2"},
		},
	}
	var out bytes.Buffer
	FormatContextEvents(events, nil, FormatOptions{VerboseBash: true}, &out)
	got := out.String()

	if !strings.Contains(got, "ok line2") {
		t.Fatalf("verbose bash in context should show full output, got:\n%s", got)
	}
}
