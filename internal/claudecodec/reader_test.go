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

// --- ADR-003 decision 1: status determination ladder ---

// TestParseLine_ToolResult_GivenExplicitSuccessField_ThenItWinsOverSniffedFailureText
// pins ladder rule 1: an explicit toolUseResult.success always wins, even when
// the result text also contains a known-failure signature (here "Exit code 1").
func TestParseLine_ToolResult_GivenExplicitSuccessField_ThenItWinsOverSniffedFailureText(t *testing.T) {
	event := parseLine(t, `{"type":"user","toolUseResult":{"success":true,"commandName":"Bash"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"Exit code 1"}]}}`)
	if event.Tool == nil || !event.Tool.Success {
		t.Fatalf("tool result = %#v, want Success=true (explicit signal wins)", event.Tool)
	}
}

// TestParseLine_ToolResult_GivenBashResultWithoutSuccessField_ThenNoSignalDefaultsOk
// pins that the highest-frequency shape (Bash toolUseResult carries no
// "success" field at all) still resolves to ok when nothing in the ladder
// signals failure. This is the safe default from ADR-003 rule 4.
func TestParseLine_ToolResult_GivenBashResultWithoutSuccessField_ThenNoSignalDefaultsOk(t *testing.T) {
	event := parseLine(t, `{"type":"user","toolUseResult":{"stdout":"done","stderr":"","interrupted":false},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"done\nExit code 0"}]}}`)
	if event.Tool == nil || !event.Tool.Success {
		t.Fatalf("tool result = %#v, want Success=true", event.Tool)
	}
}

// TestParseLine_ToolResult_GivenNoSuccessFieldAndIsErrorTrue_ThenFailed guards
// the bug described in ADR-003: Bash toolUseResult has no "success" field, so
// the codec used to default Success=true unconditionally, rendering failed
// commands as "-> ok". With is_error:true parsed, it must resolve to FAILED.
func TestParseLine_ToolResult_GivenNoSuccessFieldAndIsErrorTrue_ThenFailed(t *testing.T) {
	event := parseLine(t, `{"type":"user","toolUseResult":{"stdout":"","stderr":"no such file or directory"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","is_error":true,"content":"no such file or directory"}]}}`)
	if event.Tool == nil || event.Tool.Success {
		t.Fatalf("tool result = %#v, want Success=false", event.Tool)
	}
}

// TestParseLine_ToolResult_GivenIsErrorFalseButExitCodeNonZero_ThenSniffedAsFailed
// guards the ADR-003 negative knowledge: is_error is a one-directional signal.
// Real transcripts carry is_error:false on Bash results whose text still says
// "Exit code 1" — false must never be trusted as a success signal, so the
// content sniff step must still catch the failure.
func TestParseLine_ToolResult_GivenIsErrorFalseButExitCodeNonZero_ThenSniffedAsFailed(t *testing.T) {
	event := parseLine(t, `{"type":"user","toolUseResult":{"stdout":"","stderr":""},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","is_error":false,"content":"Exit code 1"}]}}`)
	if event.Tool == nil || event.Tool.Success {
		t.Fatalf("tool result = %#v, want Success=false (sniffed from Exit code 1)", event.Tool)
	}
}

// TestParseLine_ToolResult_GivenExitCodeZero_ThenNotSniffedAsFailed guards
// against over-eager sniffing: "Exit code 0" reports a successful exit and
// must not trigger the failure signature.
func TestParseLine_ToolResult_GivenExitCodeZero_ThenNotSniffedAsFailed(t *testing.T) {
	event := parseLine(t, `{"type":"user","toolUseResult":{"stdout":"ok","stderr":""},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok\nExit code 0"}]}}`)
	if event.Tool == nil || !event.Tool.Success {
		t.Fatalf("tool result = %#v, want Success=true", event.Tool)
	}
}

// TestParseLine_ToolResult_GivenExitCodeMentionedMidSentence_ThenNotSniffedAsFailed
// guards the nonZeroExitCodeLine regex anchor `(?m)^Exit code (\d+)\s*$`: it
// only matches a bare "Exit code N" line, not the phrase appearing inside
// other text. This protects against a future edit that loosens the anchor
// (e.g. dropping `^`/`$` or switching to a plain substring match), which
// would sniff any result mentioning "Exit code 1" as FAILED regardless of
// context.
func TestParseLine_ToolResult_GivenExitCodeMentionedMidSentence_ThenNotSniffedAsFailed(t *testing.T) {
	event := parseLine(t, `{"type":"user","toolUseResult":{"stdout":"","stderr":""},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"note: Exit code 1 is documented below\nall good"}]}}`)
	if event.Tool == nil || !event.Tool.Success {
		t.Fatalf("tool result = %#v, want Success=true (mid-sentence mention must not match the bare-line anchor)", event.Tool)
	}
}

// TestParseLine_ToolResult_GivenHookErrorText_ThenSniffedAsFailed pins the
// second ADR-003 sniff pattern: a hook rejection is reported as ordinary
// result content (no success/is_error signal) ending in "... hook error".
func TestParseLine_ToolResult_GivenHookErrorText_ThenSniffedAsFailed(t *testing.T) {
	event := parseLine(t, `{"type":"user","toolUseResult":{"stdout":"","stderr":""},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"PreToolUse:Bash [rm -rf /tmp/x] hook error: blocked by policy"}]}}`)
	if event.Tool == nil || event.Tool.Success {
		t.Fatalf("tool result = %#v, want Success=false (sniffed from hook error)", event.Tool)
	}
}

// --- ADR-003 decision 3: diff summaries for Edit/Write ---

// TestParseLine_ToolResult_GivenEditSingleHunk_ThenDiffStatSumsHunkLines pins
// the codec's structuredPatch parsing: +/- line counts summed from the hunk's
// "lines" entries, and NewStartLine taken from the hunk's newStart.
func TestParseLine_ToolResult_GivenEditSingleHunk_ThenDiffStatSumsHunkLines(t *testing.T) {
	line := `{"type":"user","toolUseResult":{"success":true,"commandName":"Edit","structuredPatch":[` +
		`{"oldStart":10,"oldLines":3,"newStart":10,"newLines":4,"lines":[" line1","-old line","+new line","+another new"," line2"]}` +
		`]},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}]}}`
	event := parseLine(t, line)

	stat := event.Tool.DiffStat
	if stat == nil {
		t.Fatal("DiffStat is nil, want populated stat")
	}
	if stat.IsNewFile {
		t.Fatal("IsNewFile = true, want false for Edit")
	}
	if stat.Additions != 2 || stat.Deletions != 1 || stat.NewStartLine != 10 || stat.HunkCount != 1 {
		t.Fatalf("DiffStat = %#v, want Additions=2 Deletions=1 NewStartLine=10 HunkCount=1", stat)
	}
}

// TestParseLine_ToolResult_GivenEditMultipleHunks_ThenDiffStatAggregatesAcrossHunks
// pins multi-hunk aggregation: additions/deletions sum across every hunk, but
// NewStartLine stays pinned to the *first* hunk (per ADR-003), and HunkCount
// reflects the total hunk count.
func TestParseLine_ToolResult_GivenEditMultipleHunks_ThenDiffStatAggregatesAcrossHunks(t *testing.T) {
	line := `{"type":"user","toolUseResult":{"success":true,"commandName":"Edit","structuredPatch":[` +
		`{"oldStart":5,"oldLines":2,"newStart":5,"newLines":3,"lines":[" a","+b","+c"]},` +
		`{"oldStart":40,"oldLines":4,"newStart":41,"newLines":2,"lines":["-d","-e"," f"]}` +
		`]},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}]}}`
	event := parseLine(t, line)

	stat := event.Tool.DiffStat
	if stat == nil {
		t.Fatal("DiffStat is nil, want populated stat")
	}
	if stat.Additions != 2 || stat.Deletions != 2 || stat.NewStartLine != 5 || stat.HunkCount != 2 {
		t.Fatalf("DiffStat = %#v, want Additions=2 Deletions=2 NewStartLine=5 HunkCount=2", stat)
	}
}

// TestParseLine_ToolResult_GivenWriteNewFile_ThenDiffStatCountsContentLines
// pins Write's new-file shape: empty structuredPatch plus a content field, no
// hunks to parse, so the line count comes from splitting content instead.
func TestParseLine_ToolResult_GivenWriteNewFile_ThenDiffStatCountsContentLines(t *testing.T) {
	line := `{"type":"user","toolUseResult":{"success":true,"commandName":"Write","structuredPatch":[],` +
		`"content":"package main\n\nfunc main() {}\n"},` +
		`"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}]}}`
	event := parseLine(t, line)

	stat := event.Tool.DiffStat
	if stat == nil {
		t.Fatal("DiffStat is nil, want populated stat")
	}
	if !stat.IsNewFile {
		t.Fatal("IsNewFile = false, want true for Write")
	}
	if stat.NewFileLines != 3 {
		t.Fatalf("NewFileLines = %d, want 3", stat.NewFileLines)
	}
}

// TestParseLine_ToolResult_GivenWriteNewFile_ThenCountContentLinesMatchesDocumentedContract
// pins the countContentLines doc comment (model.go): a trailing newline is the
// common text-file convention and must not count as an extra blank line, while
// truly empty content reports zero lines. The existing diff-stat test above
// only exercises content with a trailing newline, leaving these two boundary
// cases unguarded.
func TestParseLine_ToolResult_GivenWriteNewFile_ThenCountContentLinesMatchesDocumentedContract(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		wantNewFile   bool
		wantFileLines int
	}{
		{name: "no trailing newline counts visible lines", content: "a\nb", wantNewFile: true, wantFileLines: 2},
		{name: "empty content reports zero lines", content: "", wantNewFile: true, wantFileLines: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contentJSON, err := json.Marshal(tt.content)
			if err != nil {
				t.Fatalf("marshal content: %v", err)
			}
			line := `{"type":"user","toolUseResult":{"success":true,"commandName":"Write","structuredPatch":[],` +
				`"content":` + string(contentJSON) + `},` +
				`"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}]}}`
			event := parseLine(t, line)

			stat := event.Tool.DiffStat
			if stat == nil {
				t.Fatal("DiffStat is nil, want populated stat")
			}
			if stat.IsNewFile != tt.wantNewFile {
				t.Fatalf("IsNewFile = %v, want %v", stat.IsNewFile, tt.wantNewFile)
			}
			if stat.NewFileLines != tt.wantFileLines {
				t.Fatalf("NewFileLines = %d, want %d", stat.NewFileLines, tt.wantFileLines)
			}
		})
	}
}

// TestParseLine_ToolResult_GivenEditWithoutStructuredPatch_ThenDiffStatNil
// guards the ADR-003 fallback: a successful Edit whose toolUseResult carries
// no structuredPatch (missing/unparsable) must not synthesize a fake diff —
// DiffStat stays nil so Summary() renders the bare "-> ok" it always did.
func TestParseLine_ToolResult_GivenEditWithoutStructuredPatch_ThenDiffStatNil(t *testing.T) {
	line := `{"type":"user","toolUseResult":{"success":true,"commandName":"Edit"},` +
		`"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}]}}`
	event := parseLine(t, line)

	if event.Tool.DiffStat != nil {
		t.Fatalf("DiffStat = %#v, want nil (no structuredPatch in transcript)", event.Tool.DiffStat)
	}
}

// TestParseLine_ToolResult_GivenFailedEdit_ThenDiffStatNil guards the
// constraint that diff summaries never apply to failed results, even when the
// transcript happens to carry a structuredPatch — the existing FAILED excerpt
// path must own the summary, not the diff annotation.
func TestParseLine_ToolResult_GivenFailedEdit_ThenDiffStatNil(t *testing.T) {
	line := `{"type":"user","toolUseResult":{"success":false,"commandName":"Edit","structuredPatch":[` +
		`{"oldStart":1,"oldLines":1,"newStart":1,"newLines":1,"lines":["-a","+b"]}` +
		`]},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"permission denied"}]}}`
	event := parseLine(t, line)

	if event.Tool.DiffStat != nil {
		t.Fatalf("DiffStat = %#v, want nil for a failed result", event.Tool.DiffStat)
	}
}

// --- ADR-003 follow-up: tool_result name resolved from the preceding tool_use ---
//
// Real Claude Code transcripts carry no commandName/agentType field on
// Bash/Edit/Write/Read toolUseResults (only "success"-less bodies like
// {filePath, structuredPatch, ...} for Edit, or {type, file} for Read) — the
// tool name only exists on the earlier assistant tool_use block, correlated
// by tool_use_id. A fixture with RawName/commandName hand-filled (as every
// other test in this file does, matching ParseLine's single-line contract)
// cannot catch a regression here: it has to run the real two-entry sequence
// through ReadAll so the tool_use -> tool_result name resolution actually
// executes.

// TestReadAll_GivenEditToolResultWithoutCommandName_ThenRawNameResolvedFromPrecedingToolUse
// guards the bug where diffStatFor's tool-name gate silently no-opped on
// every real transcript because RawName was always "": with the tool_use's
// name recovered via tool_use_id, RawName must read "Edit" and the
// structuredPatch diff annotation must render.
func TestReadAll_GivenEditToolResultWithoutCommandName_ThenRawNameResolvedFromPrecedingToolUse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"assistant","timestamp":"2026-07-15T00:00:00Z","message":{"role":"assistant","content":[` +
			`{"type":"tool_use","id":"toolu_edit1","name":"Edit","input":{"file_path":"/repo/src/lib/app-metadata.ts"}}` +
			`]}}`,
		`{"type":"user","timestamp":"2026-07-15T00:00:01Z","toolUseResult":{` +
			`"filePath":"/repo/src/lib/app-metadata.ts","oldString":"a","newString":"b","originalFile":"a",` +
			`"replaceAll":false,"userModified":false,` +
			`"structuredPatch":[{"oldStart":10,"oldLines":3,"newStart":10,"newLines":4,` +
			`"lines":[" line1","-old line","+new line","+another new"," line2"]}]` +
			`},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_edit1",` +
			`"content":"The file /repo/src/lib/app-metadata.ts has been updated successfully."}]}}`,
		"",
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	events, err := ReadAll(path)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if len(events) != 2 || events[1].Tool == nil {
		t.Fatalf("events = %#v, want an assistant event followed by a tool_result event", events)
	}

	tool := events[1].Tool
	if tool.RawName != session.ToolEdit {
		t.Fatalf("RawName = %q, want %q (resolved from preceding tool_use)", tool.RawName, session.ToolEdit)
	}
	wantSummary := " -> ok (+2, -1 @ L10)"
	if got := tool.Summary(); got != wantSummary {
		t.Fatalf("Summary() = %q, want %q", got, wantSummary)
	}
}

// TestReadAll_GivenReadToolResultWithoutCommandName_ThenBareOkSuppressionApplies
// guards the companion regression: Summary()'s ADR-002 bare "-> ok"
// suppression for Read switches on RawName, so with RawName stuck at "" on
// real transcripts the tool's boilerplate confirmation text ("has been
// updated successfully...") leaked into the rendered summary instead of
// being suppressed.
func TestReadAll_GivenReadToolResultWithoutCommandName_ThenBareOkSuppressionApplies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"assistant","timestamp":"2026-07-15T00:00:00Z","message":{"role":"assistant","content":[` +
			`{"type":"tool_use","id":"toolu_read1","name":"Read","input":{"file_path":"/repo/README.md"}}` +
			`]}}`,
		`{"type":"user","timestamp":"2026-07-15T00:00:01Z","toolUseResult":{` +
			`"type":"text","file":{"content":"# README","numLines":1}` +
			`},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_read1",` +
			`"content":"     1\t# README"}]}}`,
		"",
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	events, err := ReadAll(path)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if len(events) != 2 || events[1].Tool == nil {
		t.Fatalf("events = %#v, want an assistant event followed by a tool_result event", events)
	}

	tool := events[1].Tool
	if tool.RawName != session.ToolRead {
		t.Fatalf("RawName = %q, want %q (resolved from preceding tool_use)", tool.RawName, session.ToolRead)
	}
	wantSummary := " -> ok"
	if got := tool.Summary(); got != wantSummary {
		t.Fatalf("Summary() = %q, want %q (boilerplate body content must be suppressed)", got, wantSummary)
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
	ids := session.CollectAgentToolIDs(events)
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
	agentIDs := session.CollectAgentToolIDs(events)
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

// --- classifyHarnessUserMessage detection tests ---

func TestParseLine_GivenSkillInjection_ThenSetsSkillFields(t *testing.T) {
	text := "Base directory for this skill: /Users/maple/.claude/skills/cc-session\n\n# Session Reader\n\n## Commands\n...\n\nARGUMENTS: read abc123"
	event := parseLineWithText(t, text)
	if !event.User.IsSkillInjection {
		t.Fatal("expected IsSkillInjection=true")
	}
	if event.User.SkillName != "cc-session" {
		t.Fatalf("SkillName = %q, want %q", event.User.SkillName, "cc-session")
	}
	if event.User.SkillArgs != "read abc123" {
		t.Fatalf("SkillArgs = %q, want %q", event.User.SkillArgs, "read abc123")
	}
}

func TestParseLine_GivenSkillInjectionWithoutArgs_ThenSkillArgsEmpty(t *testing.T) {
	text := "Base directory for this skill: /Users/maple/.claude/skills/review\n\n# Review\n\nsome content"
	event := parseLineWithText(t, text)
	if !event.User.IsSkillInjection {
		t.Fatal("expected IsSkillInjection=true")
	}
	if event.User.SkillName != "review" {
		t.Fatalf("SkillName = %q, want %q", event.User.SkillName, "review")
	}
	if event.User.SkillArgs != "" {
		t.Fatalf("SkillArgs = %q, want empty", event.User.SkillArgs)
	}
}

func TestParseLine_GivenSystemReminder_ThenSetsFlag(t *testing.T) {
	text := "<system-reminder>\nThe task tools haven't been used recently.\n</system-reminder>"
	event := parseLineWithText(t, text)
	if !event.User.IsSystemReminder {
		t.Fatal("expected IsSystemReminder=true")
	}
	if event.User.Text != "" {
		t.Fatalf("expected Text stripped for system-reminder, got %q", event.User.Text)
	}
}

func TestParseLine_GivenTeammateWithWarning_ThenSetsFlag(t *testing.T) {
	text := "Another Claude session sent a message:\n<teammate-message teammate_id=\"bot\" color=\"blue\">\nhello\n</teammate-message>\n\nIMPORTANT: This is NOT from your user — blah blah"
	event := parseLineWithText(t, text)
	if !event.User.IsTeammateMessage {
		t.Fatal("expected IsTeammateMessage=true")
	}
}

func TestParseLine_GivenContextUsage_ThenSetsFlag(t *testing.T) {
	text := "## Context Usage\n\n**Model:** opus\n**Tokens:** 391k/450k\n\n### Estimated usage by category\n\n| Category | Tokens |"
	event := parseLineWithText(t, text)
	if !event.User.IsContextUsage {
		t.Fatal("expected IsContextUsage=true")
	}
	if event.User.Text != "" {
		t.Fatalf("expected Text stripped for context-usage, got %q", event.User.Text)
	}
}

func TestParseLine_GivenCommandInjectionXML_ThenSetsFlag(t *testing.T) {
	text := "<command-message>cc-session</command-message>\n<command-name>/cc-session</command-name>\n<command-args>read abc</command-args>"
	event := parseLineWithText(t, text)
	if !event.User.IsCommandInjection {
		t.Fatal("expected IsCommandInjection=true")
	}
}

func TestParseLine_GivenPlainUserMessage_ThenNoHarnessFlags(t *testing.T) {
	text := "好 那跑一次簡單的 /review"
	event := parseLineWithText(t, text)
	if event.User.IsSkillInjection || event.User.IsSystemReminder || event.User.IsTeammateMessage || event.User.IsContextUsage || event.User.IsCommandInjection {
		t.Fatal("expected no harness flags on plain user message")
	}
	if event.User.Text != text {
		t.Fatalf("Text = %q, want %q", event.User.Text, text)
	}
}

func parseLineWithText(t *testing.T, text string) session.Event {
	t.Helper()
	textJSON, _ := json.Marshal(text)
	line := fmt.Sprintf(`{"type":"user","timestamp":"2026-06-17T00:00:00Z","message":{"role":"user","content":%s}}`, textJSON)
	return parseLine(t, line)
}

// --- ScanHeader: tail-read duration + noise-resilient prompt discovery ---

// TestScanHeader_GivenMultiLineTranscript_ThenEndTimestampIsLastLineTimestamp
// pins the new EndTimestamp field: ScanHeader must report the transcript's
// last timestamp (not just its first), since list.go's duration column has
// nothing else to compute from once metadata is unavailable.
func TestScanHeader_GivenMultiLineTranscript_ThenEndTimestampIsLastLineTimestamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"user","timestamp":"2026-07-15T02:00:00.000Z","message":{"role":"user","content":"start the task"}}`,
		`{"type":"assistant","timestamp":"2026-07-15T02:05:00.000Z","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`,
		"",
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	info, err := (Codec{}).ScanHeader(path)
	if err != nil {
		t.Fatalf("ScanHeader returned error: %v", err)
	}
	if info.Timestamp != "2026-07-15T02:00:00.000Z" {
		t.Fatalf("Timestamp = %q, want the first line's timestamp", info.Timestamp)
	}
	if info.EndTimestamp != "2026-07-15T02:05:00.000Z" {
		t.Fatalf("EndTimestamp = %q, want the last line's timestamp", info.EndTimestamp)
	}
}

// inheritChainNoiseLines returns the noise-only opening of a `/cc-session
// inherit` session transcript: a command invocation, a skill injection, and
// twenty tool_result turns — the shape observed in real inherit chains
// (session 2b73acc2 in the architecture note), none of which is a real human
// question. Twenty-two lines exceeds the old fixed 20-raw-line scan window.
func inheritChainNoiseLines() []string {
	lines := []string{
		`{"type":"user","timestamp":"2026-07-15T02:00:00.000Z","message":{"role":"user",` +
			`"content":"<command-message>cc-session</command-message>\n<command-name>/cc-session</command-name>\n` +
			`<command-args>inherit 9cd01951-e149-4d83-84e2-a210818d02aa</command-args>"}}`,
		`{"type":"user","timestamp":"2026-07-15T02:00:01.000Z","message":{"role":"user","content":[` +
			`{"type":"text","text":"Base directory for this skill: /Users/maple/.claude/skills/cc-session\n\n# Session Reader"}]}}`,
	}
	for i := 0; i < 20; i++ {
		lines = append(lines, fmt.Sprintf(
			`{"type":"user","timestamp":"2026-07-15T02:01:%02dZ","toolUseResult":{"success":true},`+
				`"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_%d","content":"ok"}]}}`,
			i%60, i))
	}
	return lines
}

// TestScanHeader_GivenNoiseHeavyPrefix_ThenNoiseLinesDoNotConsumeScanBudget
// guards the bug where every scanned line — including command tags, skill
// injections, and tool-result-shaped user entries — consumed one slot of the
// old fixed 20-line window. An inherit-style session opens with a command
// invocation, a skill injection, and a run of tool_result turns before the
// real human question; the window used to exhaust before ever reaching it,
// leaving [refs] sessions like 2b73acc2 with a blank list preview.
func TestScanHeader_GivenNoiseHeavyPrefix_ThenNoiseLinesDoNotConsumeScanBudget(t *testing.T) {
	lines := inheritChainNoiseLines()
	lines = append(lines,
		`{"type":"user","timestamp":"2026-07-15T02:10:00.000Z","message":{"role":"user",`+
			`"content":"可以搭配 trigger 一起看 @docs/trigger-modernization-report.md"}}`,
		"")
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	info, err := (Codec{}).ScanHeader(path)
	if err != nil {
		t.Fatalf("ScanHeader returned error: %v", err)
	}
	want := "可以搭配 trigger 一起看 @docs/trigger-modernization-report.md"
	if info.FirstUserPrompt != want {
		t.Fatalf("FirstUserPrompt = %q, want %q (noise-heavy prefix must not exhaust the scan budget)", info.FirstUserPrompt, want)
	}
}

// TestScanHeader_GivenOnlyCommandNoiseAndNoRealQuestion_ThenFallsBackToCommandPreview
// pins the layer-2 fallback: when the real question never appears within the
// scan window at all (the common case for inherit-chain sessions, where the
// real prompt can be dozens of turns later), FirstUserPrompt must still carry
// enough information to identify the session instead of staying blank.
func TestScanHeader_GivenOnlyCommandNoiseAndNoRealQuestion_ThenFallsBackToCommandPreview(t *testing.T) {
	lines := append(inheritChainNoiseLines(), "")
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	info, err := (Codec{}).ScanHeader(path)
	if err != nil {
		t.Fatalf("ScanHeader returned error: %v", err)
	}
	want := "[/cc-session inherit 9cd01951]"
	if info.FirstUserPrompt != want {
		t.Fatalf("FirstUserPrompt = %q, want %q (command-invocation fallback preview)", info.FirstUserPrompt, want)
	}
}

// TestScanHeader_GivenInterruptedToolCallSentinel_ThenTreatsItAsNoiseNotAPrompt
// guards a bug caught during real-transcript acceptance testing of session
// 2b73acc2: Claude Code inserts a fixed "[Request interrupted by user]"
// text block (same role/shape as a real question) whenever a tool call gets
// interrupted. Before this fix that sentinel was accepted as the first
// "genuine" candidate, so the list preview showed the harness's own
// boilerplate instead of falling through to the command-invocation preview.
func TestScanHeader_GivenInterruptedToolCallSentinel_ThenTreatsItAsNoiseNotAPrompt(t *testing.T) {
	lines := inheritChainNoiseLines()
	lines = append(lines,
		`{"type":"user","timestamp":"2026-07-15T02:05:14.315Z","message":{"role":"user","content":[`+
			`{"type":"text","text":"[Request interrupted by user]"}]}}`,
		"")
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	info, err := (Codec{}).ScanHeader(path)
	if err != nil {
		t.Fatalf("ScanHeader returned error: %v", err)
	}
	want := "[/cc-session inherit 9cd01951]"
	if info.FirstUserPrompt != want {
		t.Fatalf("FirstUserPrompt = %q, want %q (interrupted-request sentinel must not be mistaken for a real prompt)", info.FirstUserPrompt, want)
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
