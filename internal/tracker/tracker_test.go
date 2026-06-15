package tracker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeEntry is a test helper that appends a single entry to path.
func writeEntry(t *testing.T, path string, entry UsageEntry) {
	t.Helper()
	if err := LogUsageToPath(entry, path); err != nil {
		t.Fatalf("LogUsageToPath: %v", err)
	}
}

func TestLogUsageToPath_GivenValidEntry_WhenAppended_ThenWritesValidJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	entry := UsageEntry{
		Timestamp: "2026-06-15T10:00:00Z",
		Command:   "read",
		Target:    "abc123",
		Cwd:       "/Users/maple/Desktop",
		Caller:    "session-uuid",
	}

	if err := LogUsageToPath(entry, path); err != nil {
		t.Fatalf("LogUsageToPath returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	line := strings.TrimSpace(string(data))

	var got UsageEntry
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("line is not valid JSON: %v\nline: %q", err, line)
	}
	if got.Timestamp != entry.Timestamp {
		t.Errorf("Timestamp = %q, want %q", got.Timestamp, entry.Timestamp)
	}
	if got.Command != entry.Command {
		t.Errorf("Command = %q, want %q", got.Command, entry.Command)
	}
	if got.Target != entry.Target {
		t.Errorf("Target = %q, want %q", got.Target, entry.Target)
	}
	if got.Cwd != entry.Cwd {
		t.Errorf("Cwd = %q, want %q", got.Cwd, entry.Cwd)
	}
	if got.Caller != entry.Caller {
		t.Errorf("Caller = %q, want %q", got.Caller, entry.Caller)
	}
}

func TestLogUsageToPath_GivenMultipleEntries_WhenAppended_ThenAllLinesPresent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	entries := []UsageEntry{
		{Timestamp: "2026-06-15T10:00:00Z", Command: "read", Target: "aaa"},
		{Timestamp: "2026-06-15T10:01:00Z", Command: "stats", Target: "bbb"},
		{Timestamp: "2026-06-15T10:02:00Z", Command: "audit", Target: "ccc"},
	}

	for _, e := range entries {
		writeEntry(t, path, e)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("line count = %d, want 3\ncontents: %q", len(lines), string(data))
	}
	for i, line := range lines {
		var got UsageEntry
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("line %d is not valid JSON: %v\nline: %q", i, err, line)
		}
		if got.Target != entries[i].Target {
			t.Errorf("line %d: Target = %q, want %q", i, got.Target, entries[i].Target)
		}
	}
}

func TestLogUsageToPath_GivenMissingDir_WhenLogged_ThenCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	path := filepath.Join(dir, "usage.jsonl")

	entry := UsageEntry{Command: "read", Target: "x"}
	if err := LogUsageToPath(entry, path); err != nil {
		t.Fatalf("LogUsageToPath returned error: %v", err)
	}

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("directory was not created: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file was not created: %v", err)
	}
}

func TestReadUsageLogFromPath_GivenMultipleEntries_WhenRead_ThenReturnsReverseChronological(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	first := UsageEntry{Timestamp: "2026-06-15T09:00:00Z", Command: "read", Target: "first"}
	last := UsageEntry{Timestamp: "2026-06-15T11:00:00Z", Command: "read", Target: "last"}

	writeEntry(t, path, first)
	writeEntry(t, path, last)

	entries, err := ReadUsageLogFromPath(0, "", path)
	if err != nil {
		t.Fatalf("ReadUsageLogFromPath returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entry count = %d, want 2", len(entries))
	}
	if entries[0].Target != "last" {
		t.Errorf("entries[0].Target = %q, want %q (most recent first)", entries[0].Target, "last")
	}
	if entries[1].Target != "first" {
		t.Errorf("entries[1].Target = %q, want %q", entries[1].Target, "first")
	}
}

func TestReadUsageLogFromPath_GivenCmdFilter_WhenRead_ThenReturnsOnlyMatchingCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	writeEntry(t, path, UsageEntry{Command: "read", Target: "a"})
	writeEntry(t, path, UsageEntry{Command: "stats", Target: "b"})
	writeEntry(t, path, UsageEntry{Command: "read", Target: "c"})

	entries, err := ReadUsageLogFromPath(0, "read", path)
	if err != nil {
		t.Fatalf("ReadUsageLogFromPath returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entry count = %d, want 2 (only 'read' commands)", len(entries))
	}
	for _, e := range entries {
		if e.Command != "read" {
			t.Errorf("unexpected command %q in filtered results", e.Command)
		}
	}
}

func TestReadUsageLogFromPath_GivenLimit_WhenRead_ThenRespectsLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	for i := 0; i < 5; i++ {
		writeEntry(t, path, UsageEntry{Command: "read", Target: "entry"})
	}

	entries, err := ReadUsageLogFromPath(2, "", path)
	if err != nil {
		t.Fatalf("ReadUsageLogFromPath returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entry count = %d, want 2 (limit applied)", len(entries))
	}
}

func TestReadUsageLogFromPath_GivenMissingFile_WhenRead_ThenReturnsEmptySlice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.jsonl")

	entries, err := ReadUsageLogFromPath(0, "", path)
	if err != nil {
		t.Fatalf("ReadUsageLogFromPath returned error for missing file: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entry count = %d, want 0 for missing file", len(entries))
	}
}

func TestDetectCallerSessionWithBase_GivenMissingDir_WhenDetected_ThenReturnsEmptyString(t *testing.T) {
	projectsDir := filepath.Join(t.TempDir(), "no-such-projects")

	got := DetectCallerSessionWithBase("/Users/maple/Desktop", projectsDir)
	if got != "" {
		t.Errorf("DetectCallerSessionWithBase = %q, want empty string for missing dir", got)
	}
}

func TestDetectCallerSessionWithBase_GivenMultipleJSONL_WhenDetected_ThenReturnsNewestSession(t *testing.T) {
	projectsDir := t.TempDir()
	cwd := "/Users/maple/Desktop"

	// Claude Code maps the cwd by replacing "/" with "-".
	projectDir := filepath.Join(projectsDir, strings.ReplaceAll(cwd, "/", "-"))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}

	olderPath := filepath.Join(projectDir, "older-session-uuid.jsonl")
	newerPath := filepath.Join(projectDir, "newer-session-uuid.jsonl")

	for _, p := range []string{olderPath, newerPath} {
		if err := os.WriteFile(p, []byte{}, 0o644); err != nil {
			t.Fatalf("create %s: %v", p, err)
		}
	}

	olderTime := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	newerTime := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(olderPath, olderTime, olderTime); err != nil {
		t.Fatalf("chtimes older: %v", err)
	}
	if err := os.Chtimes(newerPath, newerTime, newerTime); err != nil {
		t.Fatalf("chtimes newer: %v", err)
	}

	got := DetectCallerSessionWithBase(cwd, projectsDir)
	if got != "newer-session-uuid" {
		t.Errorf("DetectCallerSessionWithBase = %q, want %q", got, "newer-session-uuid")
	}
}
