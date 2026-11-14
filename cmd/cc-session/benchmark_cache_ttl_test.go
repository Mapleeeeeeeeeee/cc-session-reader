package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
)

// writeBenchmarkSession stores a one-turn session whose only assistant record
// carries the given usage JSON.
func writeBenchmarkSession(t *testing.T, root string, sid string, usage string) {
	t.Helper()
	projectDir := filepath.Join(root, "projects", "proj")
	metaDir := filepath.Join(root, "session-meta")
	for _, dir := range []string{projectDir, metaDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create dir: %v", err)
		}
	}
	transcript := strings.Join([]string{
		`{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":"hi","usage":` + usage + `}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	writeListMeta(t, metaDir, sid, "/tmp/proj", "hello")
}

func runNoAPIBenchmark(t *testing.T, root string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	store := parser.Store{ProjectsDir: filepath.Join(root, "projects"), SessionMetaDir: filepath.Join(root, "session-meta")}
	args := []string{"--no-api", "--n", "5", "--min-kb", "0", "--overhead", "40000", "--model", "opus-4-8"}
	if err := runBenchmark(args, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runBenchmark returned error: %v", err)
	}
	return stdout.String()
}

// Regression: benchmark priced 1-hour cache writes at the 5-minute rate (v0.1.84)
func TestRunBenchmark_GivenSessionsWithDifferentCacheWriteTTL_ThenEachRowShowsItsOneHourShare(t *testing.T) {
	root := t.TempDir()
	writeBenchmarkSession(t, root, "11111111-1111-1111-1111-111111111111",
		`{"input_tokens":99000,"cache_creation_input_tokens":1000,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":1000},"cache_read_input_tokens":0,"output_tokens":1000}`)
	writeBenchmarkSession(t, root, "22222222-2222-2222-2222-222222222222",
		`{"input_tokens":99000,"cache_creation_input_tokens":1000,"cache_creation":{"ephemeral_5m_input_tokens":1000,"ephemeral_1h_input_tokens":0},"cache_read_input_tokens":0,"output_tokens":1000}`)

	got := runNoAPIBenchmark(t, root)

	for _, header := range []string{"=== Cost Savings Per Session", "=== Warm Cache: Cost Savings Per Session"} {
		section := outputSection(got, header)
		if !strings.Contains(section, "1h-write") {
			t.Fatalf("%q section should have a 1h-write column:\n%s", header, got)
		}
		if row := outputLineContaining(section, "11111111"); !strings.Contains(row, "100%") {
			t.Errorf("%q: all-1h session row should show 100%% 1h-write: %q", header, row)
		}
		if row := outputLineContaining(section, "22222222"); !strings.Contains(row, "  0%") {
			t.Errorf("%q: all-5m session row should show 0%% 1h-write: %q", header, row)
		}
	}
}

func TestRunBenchmark_GivenSessionWithoutCacheWriteTTLBreakdown_ThenSkipsItWithReason(t *testing.T) {
	root := t.TempDir()
	writeBenchmarkSession(t, root, "33333333-3333-3333-3333-333333333333",
		`{"input_tokens":99000,"cache_creation_input_tokens":1000,"cache_read_input_tokens":0,"output_tokens":1000}`)

	got := runNoAPIBenchmark(t, root)

	if !strings.Contains(got, "skipping 33333333: no cache write TTL breakdown in usage") {
		t.Fatalf("session without TTL breakdown should be skipped with its reason:\n%s", got)
	}
	if !strings.Contains(got, "No sessions could be processed.") {
		t.Fatalf("skipped session must not appear in the result tables:\n%s", got)
	}
}
