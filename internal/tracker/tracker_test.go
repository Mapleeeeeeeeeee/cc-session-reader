package tracker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

func TestCallerSessionIDsFromPath_GivenMixedEntries_WhenRead_ThenReturnsOnlyCallerIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	writeEntry(t, path, UsageEntry{Command: "read", Target: "session-a", Caller: "caller-uuid-1"})
	writeEntry(t, path, UsageEntry{Command: "read", Target: "session-b", Caller: ""})
	writeEntry(t, path, UsageEntry{Command: "stats", Target: "session-c", Caller: "caller-uuid-2"})
	writeEntry(t, path, UsageEntry{Command: "read", Target: "session-d", Caller: "caller-uuid-1"})
	writeEntry(t, path, UsageEntry{Command: "list", Target: "", Caller: "caller-uuid-3"})

	got := CallerSessionIDsFromPath(path)

	if !got["caller-uuid-1"] {
		t.Errorf("expected caller-uuid-1 in result")
	}
	if !got["caller-uuid-2"] {
		t.Errorf("expected caller-uuid-2 in result")
	}
	if got[""] {
		t.Errorf("empty string should not be in result")
	}
	if got["caller-uuid-3"] {
		t.Errorf("caller with empty target should not be in result")
	}
	if len(got) != 2 {
		t.Errorf("result length = %d, want 2", len(got))
	}
}

func TestCallerSessionIDsFromPath_GivenMissingFile_WhenRead_ThenReturnsEmptyMap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.jsonl")

	got := CallerSessionIDsFromPath(path)

	if len(got) != 0 {
		t.Errorf("result length = %d, want 0 for missing file", len(got))
	}
}

func TestDetectCallerSessionWithBase_GivenMissingDir_WhenDetected_ThenReturnsEmptyString(t *testing.T) {
	projectsDir := filepath.Join(t.TempDir(), "no-such-projects")

	got := DetectCallerSessionWithBase("/Users/maple/Desktop", projectsDir)
	if got != "" {
		t.Errorf("DetectCallerSessionWithBase = %q, want empty string for missing dir", got)
	}
}

func TestLogUsageToPath_GivenResultAndError_WhenAppended_ThenRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	entry := UsageEntry{Command: "read", Target: "x", Result: "error", Error: "transcript not found: abc"}

	writeEntry(t, path, entry)

	entries, err := ReadUsageLogFromPath(0, "", path)
	if err != nil {
		t.Fatalf("ReadUsageLogFromPath returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(entries))
	}
	if entries[0].Result != "error" {
		t.Errorf("Result = %q, want %q", entries[0].Result, "error")
	}
	if entries[0].Error != entry.Error {
		t.Errorf("Error = %q, want %q", entries[0].Error, entry.Error)
	}
}

func TestLogUsageToPath_GivenToolIDs_WhenAppended_ThenRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	entry := UsageEntry{Command: "expand", Target: "abc123", ToolIDs: []string{"Q1hv", "ooQF"}}

	writeEntry(t, path, entry)

	entries, err := ReadUsageLogFromPath(0, "", path)
	if err != nil {
		t.Fatalf("ReadUsageLogFromPath returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(entries))
	}
	if !reflect.DeepEqual(entries[0].ToolIDs, entry.ToolIDs) {
		t.Errorf("ToolIDs = %v, want %v", entries[0].ToolIDs, entry.ToolIDs)
	}
}

func TestLogUsageToPath_GivenNoToolIDs_WhenAppended_ThenOmitsToolIDsFromJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	entry := UsageEntry{Command: "read", Target: "abc123"}

	writeEntry(t, path, entry)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	line := strings.TrimSpace(string(data))
	if strings.Contains(line, "tool_ids") {
		t.Errorf("expected tool_ids to be omitted for a command with no tool IDs, got: %s", line)
	}
}

// Regression: entries written before the ToolIDs field existed have no
// "tool_ids" key at all. Without this, unmarshaling a legacy line must still
// succeed and yield a nil/empty slice rather than an error.
func TestReadUsageLogFromPath_GivenLegacyEntryWithoutToolIDsField_WhenRead_ThenToolIDsIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	raw := `{"ts":"2026-06-15T10:00:00Z","cmd":"expand","target":"legacy","cwd":"/x","caller":"c"}` + "\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write legacy entry: %v", err)
	}

	entries, err := ReadUsageLogFromPath(0, "", path)
	if err != nil {
		t.Fatalf("ReadUsageLogFromPath returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(entries))
	}
	if len(entries[0].ToolIDs) != 0 {
		t.Errorf("ToolIDs = %v, want empty for a pre-existing entry", entries[0].ToolIDs)
	}
}

// Regression: entries written before the Result field existed have no
// "result" key at all. Without this, an empty Result on unmarshal could be
// mistaken for a known failure instead of "we don't know".
func TestReadUsageLogFromPath_GivenLegacyEntryWithoutResultField_WhenRead_ThenResultIsEmptyNotError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	raw := `{"ts":"2026-06-15T10:00:00Z","cmd":"read","target":"legacy","cwd":"/x","caller":"c"}` + "\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write legacy entry: %v", err)
	}

	entries, err := ReadUsageLogFromPath(0, "", path)
	if err != nil {
		t.Fatalf("ReadUsageLogFromPath returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(entries))
	}
	if entries[0].Result != "" {
		t.Errorf("Result = %q, want empty (unknown) for a pre-existing entry", entries[0].Result)
	}
}

// Regression: "inject" was renamed to "inherit" in 554e57b, but binaries built
// before the rename wrote entries with Command == "inject". Without alias
// normalization, "usage -cmd inherit" can't find that history.
func TestReadUsageLogFromPath_GivenLegacyInjectEntries_WhenFilteredByInherit_ThenMatchesAndNormalizesDisplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	writeEntry(t, path, UsageEntry{Command: "inject", Target: "legacy"})
	writeEntry(t, path, UsageEntry{Command: "inherit", Target: "current"})
	writeEntry(t, path, UsageEntry{Command: "read", Target: "unrelated"})

	entries, err := ReadUsageLogFromPath(0, "inherit", path)
	if err != nil {
		t.Fatalf("ReadUsageLogFromPath returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entry count = %d, want 2 (legacy inject + current inherit)", len(entries))
	}
	for _, e := range entries {
		if e.Command != "inherit" {
			t.Errorf("Command = %q, want normalized %q", e.Command, "inherit")
		}
	}
}

// Regression: filtering by the deprecated alias itself ("-cmd inject") should
// still surface entries recorded under the current name, for anyone who
// still types the old command out of habit.
func TestReadUsageLogFromPath_GivenLegacyInjectFilter_WhenFiltered_ThenAlsoMatchesCurrentInheritEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	writeEntry(t, path, UsageEntry{Command: "inherit", Target: "current"})

	entries, err := ReadUsageLogFromPath(0, "inject", path)
	if err != nil {
		t.Fatalf("ReadUsageLogFromPath returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entry count = %d, want 1 (alias filter should also match current entries)", len(entries))
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
