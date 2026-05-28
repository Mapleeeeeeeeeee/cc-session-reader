package formatter

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
)

func TestFormatRead_WhenTranscriptHasDialogueAndToolUse_ThenWritesReadableTimeline(t *testing.T) {
	transcriptPath, _ := writeFormatterFixture(t)

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, false, &out); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}

	want := `[05-28 00:00] user:
hello

[05-28 00:00] assistant:
hi
  [Bash] Echo ok -> ok: ok

[05-28 00:00] user:
next

`
	if got := out.String(); got != want {
		t.Fatalf("FormatRead output mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatContext_WhenSessionMetadataExists_ThenWritesCompactContextWithHeader(t *testing.T) {
	transcriptPath, metaDir := writeFormatterFixture(t)

	var out bytes.Buffer
	store := parser.Store{SessionMetaDir: metaDir}
	if err := FormatContextWithStore(transcriptPath, formatterFixtureSessionID, false, &out, store); err != nil {
		t.Fatalf("FormatContext returned error: %v", err)
	}

	want := `# Session 12345678 | proj | 3m

U: hello

A: hi

  [Bash] Echo ok -> ok: ok

U: next

`
	if got := out.String(); got != want {
		t.Fatalf("FormatContext output mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatRead_WhenMaxLinesReached_ThenStopsWithTruncationMessage(t *testing.T) {
	transcriptPath, _ := writeFormatterFixture(t)

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 3, false, &out); err != nil {
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
	if err := FormatRead(transcriptPath, 0, true, &out); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}

	want := `[05-28 00:00] assistant:
delegating
  [Agent(general)] Inspect project

[05-28 00:00] agent result:
agent line 1
agent line 2

`
	if got := out.String(); got != want {
		t.Fatalf("FormatRead verbose agent output mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
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
	if err := FormatRead(transcriptPath, 0, false, &out); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}

	want := "  [Bash] -> ok: orphan output\n\n"
	if got := out.String(); got != want {
		t.Fatalf("FormatRead orphan output mismatch\nwant:\n%q\ngot:\n%q", want, got)
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
