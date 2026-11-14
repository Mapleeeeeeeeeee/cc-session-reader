package formatter_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/analyzer"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/formatter"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

// apiRequestBlobTranscript is a short conversation in which every real record
// is followed by the api-request-blob copy Claude Code (>= 2.1.300) writes of
// the request content: user, assistant with a tool_use, and system roles.
// Blobs carry no timestamp, uuid or promptSource.
const apiRequestBlobTranscript = `{"type":"user","timestamp":"2026-10-16T01:00:00Z","message":{"role":"user","content":"BLOB-USER-QUESTION"}}
{"type":"api-request-blob","sessionId":"s1","hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","message":{"role":"user","content":[{"type":"text","text":"BLOB-USER-QUESTION"}]}}
{"type":"assistant","timestamp":"2026-10-16T01:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"BLOB-ASSISTANT-REPLY"},{"type":"tool_use","name":"Bash","id":"tool-1","input":{"command":"echo blob-tool-call","description":"BLOB-TOOL-CALL"}}]}}
{"type":"api-request-blob","sessionId":"s1","hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","message":{"role":"assistant","content":[{"type":"text","text":"BLOB-ASSISTANT-REPLY"},{"type":"tool_use","name":"Bash","id":"tool-1","input":{"command":"echo blob-tool-call","description":"BLOB-TOOL-CALL"}}]}}
{"type":"user","timestamp":"2026-10-16T01:00:02Z","toolUseResult":{"success":true},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"blob-tool-output"}]}}
{"type":"api-request-blob","sessionId":"s1","hash":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","message":{"role":"system","content":[{"type":"text","text":"BLOB-SYSTEM-PROMPT"}]}}
`

func readBlobTranscript(t *testing.T) []session.Event {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(apiRequestBlobTranscript), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	events, err := claudecodec.Codec{}.ReadAll(path)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return events
}

// Regression: api-request-blob records were emitted as duplicate conversation messages (v0.1.82)
func TestIntegration_Read_GivenApiRequestBlobCopies_WhenFormatted_ThenEachMessageAppearsOnce(t *testing.T) {
	events := readBlobTranscript(t)

	var out bytes.Buffer
	if err := formatter.FormatReadEvents(events, map[string]bool{}, 0, 0, formatter.FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	got := out.String()

	wantOccurrences := map[string]int{
		"BLOB-USER-QUESTION":   1,
		"BLOB-ASSISTANT-REPLY": 1,
		"BLOB-TOOL-CALL":       1,
		"BLOB-SYSTEM-PROMPT":   0,
		"[??:??:??]":           0,
	}
	for marker, want := range wantOccurrences {
		if count := strings.Count(got, marker); count != want {
			t.Errorf("%q appears %d times, want %d\ngot:\n%s", marker, count, want, got)
		}
	}
}

// Regression: api-request-blob records were emitted as duplicate conversation messages (v0.1.82)
func TestIntegration_Stats_GivenApiRequestBlobCopies_WhenComputed_ThenToolCallsAreNotInflated(t *testing.T) {
	events := readBlobTranscript(t)

	stats := analyzer.ComputeStats(events)

	bash := stats.PerTool["Bash"]
	if bash == nil || bash.CallCount != 1 {
		t.Errorf("Bash tool calls = %+v, want CallCount 1", bash)
	}
}
