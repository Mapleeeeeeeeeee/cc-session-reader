package formatter

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func TestFormatRead_WhenTranscriptHasDialogueAndToolUse_ThenWritesReadableTimeline(t *testing.T) {
	transcriptPath, _ := writeFormatterFixture(t)

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
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
	if err := FormatContextWithStore(transcriptPath, formatterFixtureSessionID, 0, 0, FormatOptions{}, &out, store, claudecodec.Codec{}); err != nil {
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
	// Guards the new pagination format: offset N is shown in the truncation
	// message so the user knows what flag to pass to continue reading.
	transcriptPath, _ := writeFormatterFixture(t)

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 3, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}

	got := out.String()
	// First 3 lines of the user block must be present.
	if !strings.Contains(got, "[05-28 00:00] user:\nhello") {
		t.Fatalf("FormatRead truncated output missing user block\ngot:\n%q", got)
	}
	// Truncation message must name the resume offset so the user can continue.
	if !strings.Contains(got, "--- truncated at line 3") {
		t.Fatalf("FormatRead truncated output missing truncation marker\ngot:\n%q", got)
	}
	if !strings.Contains(got, "use --offset 3 to continue") {
		t.Fatalf("FormatRead truncated output missing offset continuation hint\ngot:\n%q", got)
	}
	// No assistant content should appear (it starts after line 3).
	if strings.Contains(got, "assistant:") {
		t.Fatalf("FormatRead should not emit content past maxLines\ngot:\n%q", got)
	}
}

func TestFormatRead_WhenVerboseAgents_ThenWritesFullAgentResult(t *testing.T) {
	transcriptPath, _ := writeAgentFormatterFixture(t)

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{VerboseAgents: true}, &out, claudecodec.Codec{}); err != nil {
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
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
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
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{VerboseThinking: true}, &out, claudecodec.Codec{}); err != nil {
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
	FormatContextEvents(events, nil, 0, 0, FormatOptions{}, &offOut)
	if strings.Contains(offOut.String(), thinkingFixtureFirstBlock) {
		t.Fatalf("default context output must not contain thinking text\ngot:\n%q", offOut.String())
	}

	var onOut bytes.Buffer
	FormatContextEvents(events, nil, 0, 0, FormatOptions{VerboseThinking: true}, &onOut)
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
	if err := FormatReadEvents(commandNoiseEvents(), nil, 0, 0, FormatOptions{}, &out); err != nil {
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
	if err := FormatReadEvents(commandNoiseEvents(), nil, 0, 0, FormatOptions{VerboseCommands: true}, &out); err != nil {
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
	if err := FormatContextEvents(commandNoiseEvents(), nil, 0, 0, FormatOptions{}, &out); err != nil {
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
	if err := FormatContextEvents(commandNoiseEvents(), nil, 0, 0, FormatOptions{VerboseCommands: true}, &out); err != nil {
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
	if err := FormatReadEvents(events, nil, 0, 0, FormatOptions{}, &out); err != nil {
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
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
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
	FormatReadEvents(events, nil, 0, 0, FormatOptions{VerboseBash: true}, &out)
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
	FormatReadEvents(events, nil, 0, 0, FormatOptions{VerboseBash: true}, &out)
	got := out.String()

	// Non-Bash tools should be compressed to one-line summary even with verbose-bash on
	if strings.Contains(got, "line4") {
		t.Fatalf("non-Bash tool should remain compressed with verbose-bash, got:\n%s", got)
	}
	if !strings.Contains(got, "-> ok") {
		t.Fatalf("non-Bash tool summary should contain ok status, got:\n%s", got)
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
	if err := FormatReadEvents(events, nil, 0, 0, FormatOptions{}, &out); err != nil {
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
	FormatContextEvents(events, nil, 0, 0, FormatOptions{VerboseBash: true}, &out)
	got := out.String()

	if !strings.Contains(got, "ok line2") {
		t.Fatalf("verbose bash in context should show full output, got:\n%s", got)
	}
}

// generateManyEvents produces n user-message events for pagination tests.
// Each event has a distinct message body so line counts are predictable.
func generateManyEvents(n int) []session.Event {
	events := make([]session.Event, n)
	for i := range events {
		events[i] = session.Event{
			Kind:      session.EventUserMessage,
			Timestamp: "2026-05-28T00:00:00Z",
			User:      &session.UserMessage{Text: fmt.Sprintf("message %d", i)},
		}
	}
	return events
}

// TestFormatReadEvents_GivenManyLines_WhenDefaultMaxLines_ThenTruncatesAt200
// verifies that 250 events produce exactly 200 content lines plus a truncation
// footer when maxLines=200, offset=0.
func TestFormatReadEvents_GivenManyLines_WhenDefaultMaxLines_ThenTruncatesAt200(t *testing.T) {
	// Each user message renders as 3 output lines:
	//   [05-28 00:00] user:
	//   message N
	//   (blank)
	// 250 messages → 750 total lines. maxLines=200 must cut at line 200.
	events := generateManyEvents(250)
	var out bytes.Buffer
	if err := FormatReadEvents(events, nil, 200, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	got := out.String()

	lines := strings.Split(got, "\n")
	// The last element after split on trailing newline is empty; strip it.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Truncation line is the last line; content lines are everything before the
	// blank line separating content from the truncation footer.
	if !strings.Contains(got, "--- truncated at line 200") {
		t.Fatalf("expected truncation at line 200, got:\n%s", got)
	}
	if !strings.Contains(got, "use --offset 200 to continue") {
		t.Fatalf("expected --offset 200 hint in truncation message, got:\n%s", got)
	}
	// The last content line before the truncation block should be exactly line 199
	// (0-indexed). Line 199 = message 66 (199/3 = 66 rem 1, so it's the body
	// of the 67th message). Just assert we don't see message 67 or later.
	if strings.Contains(got, "message 67") {
		t.Fatalf("output past maxLines boundary — message 67 should not appear:\n%s", got)
	}
}

// TestFormatReadEvents_GivenOffset_WhenUnlimited_ThenSkipsFirstNLines
// verifies that offset=5 drops the first 5 output lines and the rest is intact.
func TestFormatReadEvents_GivenOffset_WhenUnlimited_ThenSkipsFirstNLines(t *testing.T) {
	// 5 messages at 3 lines each = 15 output lines.
	// Lines 0-2: message 0 block; lines 3-5: message 1 block; etc.
	// Skipping 5 lines means we lose message 0's full block (lines 0-2) and
	// the first 2 lines of message 1's block, starting at line 5 ("message 1").
	events := generateManyEvents(5)
	var full, withOffset bytes.Buffer
	if err := FormatReadEvents(events, nil, 0, 0, FormatOptions{}, &full); err != nil {
		t.Fatalf("FormatReadEvents (full) returned error: %v", err)
	}
	if err := FormatReadEvents(events, nil, 0, 5, FormatOptions{}, &withOffset); err != nil {
		t.Fatalf("FormatReadEvents (offset=5) returned error: %v", err)
	}

	allLines := strings.Split(full.String(), "\n")
	got := withOffset.String()

	// First line of offset output must equal allLines[5].
	firstLine := strings.SplitN(got, "\n", 2)[0]
	if firstLine != allLines[5] {
		t.Fatalf("offset=5 first line mismatch\nwant: %q\ngot:  %q", allLines[5], firstLine)
	}
	// "message 0" is on line 1 (the body of the first block). It must not appear
	// in the offset output. "message 0" is unique enough to check.
	if strings.Contains(got, "message 0") {
		t.Fatalf("offset=5 output must not contain message 0 (line 1):\n%s", got)
	}
	// "message 4" (last message) must still be present.
	if !strings.Contains(got, "message 4") {
		t.Fatalf("offset=5 output must contain message 4:\n%s", got)
	}
}

// TestFormatReadEvents_GivenOffsetAndMaxLines_WhenCombined_ThenWindowsCorrectly
// verifies that offset=2 + maxLines=3 returns exactly lines 2,3,4 of the
// full output.
func TestFormatReadEvents_GivenOffsetAndMaxLines_WhenCombined_ThenWindowsCorrectly(t *testing.T) {
	events := generateManyEvents(5) // 15 output lines
	var full, windowed bytes.Buffer
	if err := FormatReadEvents(events, nil, 0, 0, FormatOptions{}, &full); err != nil {
		t.Fatalf("FormatReadEvents (full) error: %v", err)
	}
	if err := FormatReadEvents(events, nil, 3, 2, FormatOptions{}, &windowed); err != nil {
		t.Fatalf("FormatReadEvents (windowed) error: %v", err)
	}

	allLines := strings.Split(full.String(), "\n")
	windowedLines := strings.Split(windowed.String(), "\n")

	// Strip the trailing blank from the split.
	if len(windowedLines) > 0 && windowedLines[len(windowedLines)-1] == "" {
		windowedLines = windowedLines[:len(windowedLines)-1]
	}

	// Last line is the truncation message; strip it.
	if len(windowedLines) > 0 && strings.HasPrefix(windowedLines[len(windowedLines)-1], "---") {
		windowedLines = windowedLines[:len(windowedLines)-1]
	}
	// The blank separator before the truncation message.
	if len(windowedLines) > 0 && windowedLines[len(windowedLines)-1] == "" {
		windowedLines = windowedLines[:len(windowedLines)-1]
	}

	// Content lines must be exactly allLines[2:5].
	if len(windowedLines) != 3 {
		t.Fatalf("expected 3 content lines, got %d: %q", len(windowedLines), windowedLines)
	}
	for i, want := range allLines[2:5] {
		if windowedLines[i] != want {
			t.Fatalf("windowed line %d mismatch\nwant: %q\ngot:  %q", i, want, windowedLines[i])
		}
	}
}

// TestFormatReadEvents_GivenZeroOffset_WhenSameAsNoOffset_ThenOutputIdentical
// verifies that offset=0 is a no-op and produces the same output as the
// unparameterized baseline.
func TestFormatReadEvents_GivenZeroOffset_WhenSameAsNoOffset_ThenOutputIdentical(t *testing.T) {
	events := generateManyEvents(3)
	var noOffset, zeroOffset bytes.Buffer
	if err := FormatReadEvents(events, nil, 0, 0, FormatOptions{}, &noOffset); err != nil {
		t.Fatalf("FormatReadEvents (no offset) error: %v", err)
	}
	if err := FormatReadEvents(events, nil, 0, 0, FormatOptions{}, &zeroOffset); err != nil {
		t.Fatalf("FormatReadEvents (offset=0) error: %v", err)
	}
	if noOffset.String() != zeroOffset.String() {
		t.Fatalf("offset=0 output differs from no-offset output\nno-offset:\n%q\noffset=0:\n%q",
			noOffset.String(), zeroOffset.String())
	}
}

// TestFormatReadEvents_GivenZeroMaxLines_WhenUnlimited_ThenNoTruncationMessage
// verifies that maxLines=0 emits all content with no truncation footer.
// This guards backward compatibility: existing callers that pass 0 must keep
// getting unlimited output.
func TestFormatReadEvents_GivenZeroMaxLines_WhenUnlimited_ThenNoTruncationMessage(t *testing.T) {
	events := generateManyEvents(10) // 30 lines
	var out bytes.Buffer
	if err := FormatReadEvents(events, nil, 0, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "truncated") {
		t.Fatalf("maxLines=0 must not truncate; got truncation message:\n%s", got)
	}
	// All 10 messages must appear.
	for i := 0; i < 10; i++ {
		if !strings.Contains(got, fmt.Sprintf("message %d", i)) {
			t.Fatalf("message %d missing in unlimited output:\n%s", i, got)
		}
	}
}

func TestAppendToolResult_WhenParallelToolCalls_ThenMatchesByToolUseID(t *testing.T) {
	tests := []struct {
		name        string
		pending     []pendingTool
		result      session.ToolResult
		wantMatch   int // index of pending tool that should receive the result (-1 = new orphan appended)
		wantNoMatch int // index that should NOT receive the result (-1 = skip check)
	}{
		{
			name: "given matching ID then attaches to correct pending tool",
			pending: []pendingTool{
				{toolUseID: "aaa", summary: "[Read] main.go", name: "Read"},
				{toolUseID: "bbb", summary: "[Read] util.go", name: "Read"},
			},
			result:      session.ToolResult{ToolUseID: "aaa", Success: true},
			wantMatch:   0,
			wantNoMatch: 1,
		},
		{
			name: "given empty ID then falls back to last pending tool",
			pending: []pendingTool{
				{toolUseID: "aaa", summary: "[Read] main.go", name: "Read"},
				{toolUseID: "bbb", summary: "[Read] util.go", name: "Read"},
			},
			result:      session.ToolResult{ToolUseID: "", Success: true},
			wantMatch:   1,
			wantNoMatch: 0,
		},
		{
			name: "given non-matching ID then falls back to last pending tool",
			pending: []pendingTool{
				{toolUseID: "aaa", summary: "[Read] main.go", name: "Read"},
				{toolUseID: "bbb", summary: "[Read] util.go", name: "Read"},
			},
			result:      session.ToolResult{ToolUseID: "zzz", Success: true},
			wantMatch:   1,
			wantNoMatch: 0,
		},
		{
			name:        "given no pending tools then creates orphan entry",
			pending:     []pendingTool{},
			result:      session.ToolResult{ToolUseID: "aaa", Success: true, RawName: "Bash"},
			wantMatch:   -1,
			wantNoMatch: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pending := make([]pendingTool, len(tt.pending))
			copy(pending, tt.pending)

			appendToolResult(&tt.result, &pending, FormatOptions{})

			if tt.wantMatch == -1 {
				if len(pending) != 1 {
					t.Fatalf("expected 1 orphan entry, got %d", len(pending))
				}
				if !strings.Contains(pending[0].summary, "-> ok") {
					t.Fatalf("orphan entry missing result summary, got: %q", pending[0].summary)
				}
				return
			}

			if !strings.Contains(pending[tt.wantMatch].summary, "-> ok") {
				t.Fatalf("pending[%d] should have result summary, got: %q", tt.wantMatch, pending[tt.wantMatch].summary)
			}
			if tt.wantNoMatch >= 0 && strings.Contains(pending[tt.wantNoMatch].summary, "-> ok") {
				t.Fatalf("pending[%d] should NOT have result summary, got: %q", tt.wantNoMatch, pending[tt.wantNoMatch].summary)
			}
		})
	}
}

// TestFormatReadEvents_GivenOffsetExceedsTotal_WhenFormatted_ThenEmitsOffsetExceedsMessage
// verifies that an offset past the end of the content produces a clear message
// rather than silently emitting empty output.
func TestFormatReadEvents_GivenOffsetExceedsTotal_WhenFormatted_ThenEmitsOffsetExceedsMessage(t *testing.T) {
	events := generateManyEvents(2) // 6 lines
	var out bytes.Buffer
	if err := FormatReadEvents(events, nil, 0, 9999, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "offset 9999 exceeds total") {
		t.Fatalf("expected offset-exceeds message, got:\n%q", got)
	}
}

func TestParseCCSessionCommand(t *testing.T) {
	tests := []struct {
		name        string
		cmd         string
		wantSub     string
		wantSession string
	}{
		{
			name:        "given inherit with full uuid then returns inherit and 8-char prefix",
			cmd:         "cc-session inherit 16d06326-977b-4c82-a38f-9b2358aa80ca",
			wantSub:     "inherit",
			wantSession: "16d06326",
		},
		{
			name:        "given legacy inject with full uuid then returns inject and 8-char prefix",
			cmd:         "cc-session inject 16d06326-977b-4c82-a38f-9b2358aa80ca",
			wantSub:     "inject",
			wantSession: "16d06326",
		},
		{
			name:        "given read with full uuid then returns read and 8-char prefix",
			cmd:         "cc-session read 16d06326-977b-4c82-a38f-9b2358aa80ca",
			wantSub:     "read",
			wantSession: "16d06326",
		},
		{
			name:        "given context with full uuid then returns context and 8-char prefix",
			cmd:         "cc-session context 16d06326-977b-4c82-a38f-9b2358aa80ca",
			wantSub:     "context",
			wantSession: "16d06326",
		},
		{
			name:        "given short session id then returns full id",
			cmd:         "cc-session inject abc",
			wantSub:     "inject",
			wantSession: "abc",
		},
		{
			name:        "given non cc-session command then returns empty",
			cmd:         "go build ./...",
			wantSub:     "",
			wantSession: "",
		},
		{
			name:        "given cc-session with unknown subcommand then returns empty",
			cmd:         "cc-session benchmark 16d06326-977b-4c82-a38f-9b2358aa80ca",
			wantSub:     "",
			wantSession: "",
		},
		{
			name:        "given cc-session with trailing args then still parses",
			cmd:         "cc-session inject 16d06326-977b-4c82-a38f-9b2358aa80ca --flag",
			wantSub:     "inject",
			wantSession: "16d06326",
		},
		{
			name:        "given flag as session id then returns empty",
			cmd:         "cc-session read -h",
			wantSub:     "",
			wantSession: "",
		},
		{
			name:        "given flag with value as session id then returns empty",
			cmd:         "cc-session read -max-lines 200",
			wantSub:     "",
			wantSession: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSub, gotSession := parseCCSessionCommand(tt.cmd)
			if gotSub != tt.wantSub || gotSession != tt.wantSession {
				t.Fatalf("parseCCSessionCommand(%q) = (%q, %q), want (%q, %q)",
					tt.cmd, gotSub, gotSession, tt.wantSub, tt.wantSession)
			}
		})
	}
}

func TestParseTotalLines(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{
			name: "given inject result with page marker then extracts total",
			text: "ok: [page 1/4 | lines 1-377 of 1320]",
			want: 1320,
		},
		{
			name: "given last page marker then extracts total",
			text: "ok: [page 4/4 | lines 1052-1320 of 1320]",
			want: 1320,
		},
		{
			name: "given text without page marker then returns zero",
			text: "ok",
			want: 0,
		},
		{
			name: "given empty text then returns zero",
			text: "",
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseTotalLines(tt.text); got != tt.want {
				t.Fatalf("parseTotalLines(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}

// makeInjectEvents builds a sequence of Bash tool-use/result events for a
// paginated "cc-session <subcommand> <sessionID>" call, e.g. "inherit" (current)
// or "inject" (legacy, kept for old transcripts).
func makeCCSessionEvents(subcommand, sessionID string, pages int) []session.Event {
	events := make([]session.Event, 0, pages*2)
	for p := 1; p <= pages; p++ {
		toolID := fmt.Sprintf("%s-tool-%d", subcommand, p)
		events = append(events,
			session.Event{
				Kind:      session.EventAssistantMessage,
				Timestamp: "2026-05-28T00:00:00Z",
				Assistant: &session.AssistantMessage{
					ToolUses: []session.ToolUse{
						{
							ID:   toolID,
							Name: "Bash",
							Input: session.ToolInput{Raw: map[string]any{
								"command":     fmt.Sprintf("cc-session %s %s", subcommand, sessionID),
								"description": "Load session",
							}},
						},
					},
				},
			},
			session.Event{
				Kind: session.EventToolResult,
				Tool: &session.ToolResult{
					ToolUseID: toolID,
					Success:   true,
					Text:      fmt.Sprintf("ok: [page %d/%d | lines 1-100 of 400]", p, pages),
				},
			},
		)
	}
	return events
}

// makeInjectEvents is a convenience wrapper for the legacy "cc-session inject"
// verb, used by tests that specifically cover pre-rename transcripts.
func makeInjectEvents(sessionID string, pages int) []session.Event {
	return makeCCSessionEvents("inject", sessionID, pages)
}

func TestFormatReadEvents_GivenConsecutiveInjectCalls_WhenRendered_ThenCollapsesIntoOneLine(t *testing.T) {
	sessionID := "16d06326-977b-4c82-a38f-9b2358aa80ca"
	events := makeInjectEvents(sessionID, 4)

	var out bytes.Buffer
	if err := FormatReadEvents(events, nil, 0, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	got := out.String()

	want := "(cc-session#ol-1: injected session 16d06326 here, 400 lines omitted)"
	if !strings.Contains(got, want) {
		t.Fatalf("expected collapsed inject line\nwant substring: %q\ngot:\n%s", want, got)
	}
	if strings.Contains(got, "[Bash") {
		t.Fatalf("individual inject Bash entries must not appear in output\ngot:\n%s", got)
	}
	if strings.Count(got, "cc-session#") > 1 {
		t.Fatalf("must collapse to exactly one inject summary line\ngot:\n%s", got)
	}
}

func TestFormatReadEvents_GivenConsecutiveInheritCalls_WhenRendered_ThenCollapsesWithInheritedVerb(t *testing.T) {
	sessionID := "16d06326-977b-4c82-a38f-9b2358aa80ca"
	events := makeCCSessionEvents("inherit", sessionID, 4)

	var out bytes.Buffer
	if err := FormatReadEvents(events, nil, 0, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	got := out.String()

	want := "(cc-session#ol-1: inherited session 16d06326 here, 400 lines omitted)"
	if !strings.Contains(got, want) {
		t.Fatalf("expected collapsed inherit line with 'inherited' verb\nwant substring: %q\ngot:\n%s", want, got)
	}
	if strings.Contains(got, "[Bash") {
		t.Fatalf("individual inherit Bash entries must not appear in output\ngot:\n%s", got)
	}
	if strings.Count(got, "cc-session#") > 1 {
		t.Fatalf("must collapse to exactly one inherit summary line\ngot:\n%s", got)
	}
}

func TestFormatReadEvents_GivenMixedToolsWithInject_WhenRendered_ThenOnlyCollapsesInjectOnes(t *testing.T) {
	sessionID := "16d06326-977b-4c82-a38f-9b2358aa80ca"
	injectEvents := makeInjectEvents(sessionID, 2)
	mixed := []session.Event{
		{
			Kind:      session.EventAssistantMessage,
			Timestamp: "2026-05-28T00:00:00Z",
			Assistant: &session.AssistantMessage{
				Text: "starting",
				ToolUses: []session.ToolUse{
					{ID: "read-1", Name: "Read", Input: session.ToolInput{Raw: map[string]any{"file_path": "/foo/bar.go"}}},
				},
			},
		},
		{Kind: session.EventToolResult, Tool: &session.ToolResult{ToolUseID: "read-1", Success: true, Text: "content"}},
	}
	mixed = append(mixed, injectEvents...)

	var out bytes.Buffer
	if err := FormatReadEvents(mixed, nil, 0, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, "[Read") {
		t.Fatalf("Read tool summary must still appear\ngot:\n%s", got)
	}
	if !strings.Contains(got, "(cc-session#ol-1: injected session 16d06326 here") {
		t.Fatalf("inject calls must be collapsed\ngot:\n%s", got)
	}
	if strings.Count(got, "cc-session#") > 1 {
		t.Fatalf("inject calls must collapse to one line\ngot:\n%s", got)
	}
}

func TestFormatContextEvents_GivenConsecutiveInjectCalls_WhenRendered_ThenCollapsesIntoOneLine(t *testing.T) {
	sessionID := "16d06326-977b-4c82-a38f-9b2358aa80ca"
	events := makeInjectEvents(sessionID, 4)

	var out bytes.Buffer
	if err := FormatContextEvents(events, nil, 0, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatContextEvents returned error: %v", err)
	}
	got := out.String()

	want := "(cc-session#ol-1: injected session 16d06326 here, 400 lines omitted)"
	if !strings.Contains(got, want) {
		t.Fatalf("expected collapsed inject line in context output\nwant substring: %q\ngot:\n%s", want, got)
	}
	if strings.Count(got, "cc-session#") > 1 {
		t.Fatalf("must collapse to exactly one inject summary line\ngot:\n%s", got)
	}
}

func TestFormatReadEvents_GivenCCSessionRead_WhenRendered_ThenCollapsesWithLoadedVerb(t *testing.T) {
	sessionID := "abcdef12-0000-0000-0000-000000000000"
	toolID := "read-tool-1"
	events := []session.Event{
		{
			Kind:      session.EventAssistantMessage,
			Timestamp: "2026-05-28T00:00:00Z",
			Assistant: &session.AssistantMessage{
				ToolUses: []session.ToolUse{
					{
						ID:   toolID,
						Name: "Bash",
						Input: session.ToolInput{Raw: map[string]any{
							"command":     "cc-session read " + sessionID,
							"description": "Read session",
						}},
					},
				},
			},
		},
		{Kind: session.EventToolResult, Tool: &session.ToolResult{ToolUseID: toolID, Success: true, Text: "session content"}},
	}

	var out bytes.Buffer
	if err := FormatReadEvents(events, nil, 0, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	got := out.String()

	want := "(cc-session#ol-1: loaded session abcdef12 here)"
	if !strings.Contains(got, want) {
		t.Fatalf("cc-session read must use 'loaded' verb\nwant substring: %q\ngot:\n%s", want, got)
	}
}

func TestFormatReadEvents_GivenFailedInjectCall_WhenRendered_ThenShowsNormalBashLine(t *testing.T) {
	// Guards against failed inject being collapsed into a "(cc-session: injected ...)"
	// success summary — a failed call must render as a normal [Bash#...] failure line.
	sessionID := "16d06326-977b-4c82-a38f-9b2358aa80ca"
	toolID := "inject-tool-1"
	events := []session.Event{
		{
			Kind:      session.EventAssistantMessage,
			Timestamp: "2026-05-28T00:00:00Z",
			Assistant: &session.AssistantMessage{
				ToolUses: []session.ToolUse{
					{
						ID:   toolID,
						Name: "Bash",
						Input: session.ToolInput{Raw: map[string]any{
							"command":     "cc-session inject " + sessionID,
							"description": "Inject session",
						}},
					},
				},
			},
		},
		{
			Kind: session.EventToolResult,
			Tool: &session.ToolResult{ToolUseID: toolID, Success: false, Text: "command not found: cc-session"},
		},
	}

	var out bytes.Buffer
	if err := FormatReadEvents(events, nil, 0, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "(cc-session") {
		t.Fatalf("failed inject must not be collapsed into cc-session summary\ngot:\n%s", got)
	}
	if !strings.Contains(got, "[Bash") {
		t.Fatalf("failed inject must render as a normal Bash line\ngot:\n%s", got)
	}
}

func TestFormatReadEvents_GivenVerboseBash_WhenInjectCalls_ThenSkipsCollapse(t *testing.T) {
	// Guards against -verbose-bash being bypassed: collapseCCSessionTools must not
	// run when VerboseBash is set, so the full Bash output built by appendToolResult
	// is preserved instead of being discarded by the collapse.
	sessionID := "16d06326-977b-4c82-a38f-9b2358aa80ca"
	events := makeInjectEvents(sessionID, 2)

	var out bytes.Buffer
	if err := FormatReadEvents(events, nil, 0, 0, FormatOptions{VerboseBash: true}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "(cc-session") {
		t.Fatalf("verbose-bash must not collapse inject calls\ngot:\n%s", got)
	}
	if !strings.Contains(got, "[Bash") {
		t.Fatalf("verbose-bash must render individual Bash lines\ngot:\n%s", got)
	}
}
