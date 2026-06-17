package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
)

func TestFormatTimestamp(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "ISO 8601 with Z suffix", input: "2025-03-15T14:30:00Z", want: "03-15 14:30"},
		{name: "ISO 8601 with positive offset", input: "2025-03-15T14:30:00+08:00", want: "03-15 14:30"},
		{name: "ISO 8601 with negative offset", input: "2025-12-01T09:05:00-05:00", want: "12-01 09:05"},
		{name: "ISO 8601 with milliseconds", input: "2025-06-20T23:59:59.123+00:00", want: "06-20 23:59"},
		{name: "ISO 8601 with microseconds", input: "2025-01-01T00:00:00.000000+00:00", want: "01-01 00:00"},
		{name: "invalid string returns placeholder", input: "not-a-timestamp", want: "??-?? ??:??"},
		{name: "empty string returns placeholder", input: "", want: "??-?? ??:??"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatTimestamp(tt.input)
			if got != tt.want {
				t.Fatalf("FormatTimestamp(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStoreFindTranscript(t *testing.T) {
	root := t.TempDir()
	projectsDir := filepath.Join(root, "projects", "proj")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}
	sid := "12345678-1234-1234-1234-123456789abc"
	wantPath := filepath.Join(projectsDir, sid+".jsonl")
	writeFile(t, wantPath, "")

	got, err := (Store{ProjectsDir: filepath.Join(root, "projects")}).FindTranscript(sid)
	if err != nil {
		t.Fatalf("FindTranscript returned error: %v", err)
	}
	if got != wantPath {
		t.Fatalf("FindTranscript = %q, want %q", got, wantPath)
	}
}

func TestStoreFindTranscript_WhenProjectsDirIsMissing_ThenReturnsError(t *testing.T) {
	_, err := (Store{ProjectsDir: filepath.Join(t.TempDir(), "missing")}).FindTranscript("abc")
	if err == nil {
		t.Fatal("FindTranscript returned nil error, want walk error")
	}
	if !strings.Contains(err.Error(), "walk projects dir") {
		t.Fatalf("error = %v, want walk projects dir", err)
	}
}

func TestStoreResolveSession_WhenPrefixMatchesOne_ThenReturnsBothIDAndPath(t *testing.T) {
	root := t.TempDir()
	projectsDir := filepath.Join(root, "projects", "proj")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}
	sid := "12345678-1234-1234-1234-123456789abc"
	wantPath := filepath.Join(projectsDir, sid+".jsonl")
	writeFile(t, wantPath, "")

	store := Store{ProjectsDir: filepath.Join(root, "projects")}
	got, err := store.ResolveSession("12345678")
	if err != nil {
		t.Fatalf("ResolveSession returned error: %v", err)
	}
	if got.ID != sid {
		t.Fatalf("ResolveSession().ID = %q, want %q", got.ID, sid)
	}
	if got.Path != wantPath {
		t.Fatalf("ResolveSession().Path = %q, want %q", got.Path, wantPath)
	}
}

func TestStoreResolveSession_WhenFullUUID_ThenReturnsBothIDAndPath(t *testing.T) {
	root := t.TempDir()
	projectsDir := filepath.Join(root, "projects", "proj")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}
	sid := "12345678-1234-1234-1234-123456789abc"
	wantPath := filepath.Join(projectsDir, sid+".jsonl")
	writeFile(t, wantPath, "")

	store := Store{ProjectsDir: filepath.Join(root, "projects")}
	got, err := store.ResolveSession(sid)
	if err != nil {
		t.Fatalf("ResolveSession returned error: %v", err)
	}
	if got.ID != sid {
		t.Fatalf("ResolveSession().ID = %q, want %q", got.ID, sid)
	}
	if got.Path != wantPath {
		t.Fatalf("ResolveSession().Path = %q, want %q", got.Path, wantPath)
	}
}

func TestStoreResolveSession_WhenPrefixIsAmbiguous_ThenReturnsError(t *testing.T) {
	root := t.TempDir()
	projectsDir := filepath.Join(root, "projects", "proj")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}
	writeFile(t, filepath.Join(projectsDir, "12345678-0000-0000-0000-000000000000.jsonl"), "")
	writeFile(t, filepath.Join(projectsDir, "12345678-1111-1111-1111-111111111111.jsonl"), "")

	store := Store{ProjectsDir: filepath.Join(root, "projects")}
	_, err := store.ResolveSession("12345678")
	if err == nil {
		t.Fatal("ResolveSession returned nil error, want ambiguous error")
	}
	if !strings.Contains(err.Error(), "ambiguous prefix") {
		t.Fatalf("error = %v, want ambiguous prefix", err)
	}
}

// Guards against the bug where the same session UUID living in multiple project
// dirs (worktrees reuse the session ID) was counted as multiple ambiguous
// candidates, making `read <prefix>` fail with a bogus "ambiguous prefix" error
// that listed the same UUID twice.
func TestStoreResolveSession_WhenSameUUIDInMultipleProjectDirs_ThenResolvesNotAmbiguous(t *testing.T) {
	root := t.TempDir()
	projectsDir := filepath.Join(root, "projects")
	worktreeDir := filepath.Join(projectsDir, "-Users-x-worktrees-feature")
	mainDir := filepath.Join(projectsDir, "-Users-x-main")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("create worktree dir: %v", err)
	}
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		t.Fatalf("create main dir: %v", err)
	}
	sid := "03db3dbe-0c12-1234-1234-123456789abc"
	writeFile(t, filepath.Join(worktreeDir, sid+".jsonl"), "")
	writeFile(t, filepath.Join(mainDir, sid+".jsonl"), "")

	store := Store{ProjectsDir: projectsDir}
	got, err := store.ResolveSession("03db3dbe")
	if err != nil {
		t.Fatalf("ResolveSession returned error: %v, want no error (same UUID is not ambiguous)", err)
	}
	if got.ID != sid {
		t.Fatalf("ResolveSession().ID = %q, want %q", got.ID, sid)
	}
	if got.Path == "" {
		t.Fatal("ResolveSession().Path is empty, want a transcript path")
	}
	if filepath.Base(got.Path) != sid+".jsonl" {
		t.Fatalf("ResolveSession().Path = %q, want a file named %q", got.Path, sid+".jsonl")
	}
}

// Guards the converse of the dedup fix: distinct UUIDs sharing a prefix remain a
// real conflict, but each UUID must appear only once in the error message.
func TestStoreResolveSession_WhenDistinctUUIDsShareAndDuplicate_ThenErrorListsEachUUIDOnce(t *testing.T) {
	root := t.TempDir()
	projectsDir := filepath.Join(root, "projects")
	dirA := filepath.Join(projectsDir, "-Users-x-worktrees-feature")
	dirB := filepath.Join(projectsDir, "-Users-x-main")
	if err := os.MkdirAll(dirA, 0o755); err != nil {
		t.Fatalf("create dir A: %v", err)
	}
	if err := os.MkdirAll(dirB, 0o755); err != nil {
		t.Fatalf("create dir B: %v", err)
	}
	sidOne := "12345678-0000-0000-0000-000000000000"
	sidTwo := "12345678-1111-1111-1111-111111111111"
	// sidOne appears in both dirs (the worktree-duplicate case); sidTwo is distinct.
	writeFile(t, filepath.Join(dirA, sidOne+".jsonl"), "")
	writeFile(t, filepath.Join(dirB, sidOne+".jsonl"), "")
	writeFile(t, filepath.Join(dirA, sidTwo+".jsonl"), "")

	store := Store{ProjectsDir: projectsDir}
	_, err := store.ResolveSession("12345678")
	if err == nil {
		t.Fatal("ResolveSession returned nil error, want ambiguous error for two distinct UUIDs")
	}
	if !strings.Contains(err.Error(), "ambiguous prefix") {
		t.Fatalf("error = %v, want ambiguous prefix", err)
	}
	if got := strings.Count(err.Error(), sidOne[:12]); got != 1 {
		t.Fatalf("error lists %q %d times, want exactly 1: %v", sidOne[:12], got, err)
	}
	if got := strings.Count(err.Error(), sidTwo[:12]); got != 1 {
		t.Fatalf("error lists %q %d times, want exactly 1: %v", sidTwo[:12], got, err)
	}
}

func TestStoreResolveSession_WhenPrefixHasNoMatch_ThenReturnsError(t *testing.T) {
	root := t.TempDir()
	projectsDir := filepath.Join(root, "projects")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}

	store := Store{ProjectsDir: projectsDir}
	_, err := store.ResolveSession("notfound")
	if err == nil {
		t.Fatal("ResolveSession returned nil error, want not found error")
	}
	if !strings.Contains(err.Error(), "session prefix not found") {
		t.Fatalf("error = %v, want session prefix not found", err)
	}
}

// Regression (F3): an empty prefix used to fall through to the prefix walk,
// matching every session and surfacing a misleading "ambiguous prefix ”"
// error. ResolveSession is the single choke point for all commands that accept
// a session_id, so it must reject "" up front with a clear "required" message
// and must not mention ambiguity. The walk must not even run (no projects dir
// configured here proves the empty check short-circuits before any filesystem
// access).
func TestStoreResolveSession_WhenPrefixIsEmpty_ThenReturnsRequiredError(t *testing.T) {
	store := Store{}
	_, err := store.ResolveSession("")
	if err == nil {
		t.Fatal("ResolveSession(\"\") returned nil error, want required error")
	}
	if !strings.Contains(err.Error(), "session_id is required") {
		t.Fatalf("error = %v, want 'session_id is required'", err)
	}
	if strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error = %v, must not mention 'ambiguous'", err)
	}
}

func TestStoreLoadSessionMeta(t *testing.T) {
	root := t.TempDir()
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}
	sid := "12345678-1234-1234-1234-123456789abc"
	writeFile(t, filepath.Join(metaDir, sid+".json"), `{"session_id":"`+sid+`","duration_minutes":3}`)

	meta, err := (Store{SessionMetaDir: metaDir}).LoadSessionMeta(sid)
	if err != nil {
		t.Fatalf("LoadSessionMeta returned error: %v", err)
	}
	if meta["session_id"] != sid {
		t.Fatalf("session_id = %#v, want %q", meta["session_id"], sid)
	}
}

func TestStoreLoadSessionMeta_WhenJSONIsInvalid_ThenReturnsError(t *testing.T) {
	root := t.TempDir()
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}
	sid := "12345678-1234-1234-1234-123456789abc"
	writeFile(t, filepath.Join(metaDir, sid+".json"), `{"session_id":`)

	_, err := (Store{SessionMetaDir: metaDir}).LoadSessionMeta(sid)
	if err == nil {
		t.Fatal("LoadSessionMeta returned nil error, want parse error")
	}
	if !strings.Contains(err.Error(), "parse session meta") {
		t.Fatalf("error = %v, want parse session meta", err)
	}
}

func TestStoreListSessionMetaFiles_SortsNewestFirst(t *testing.T) {
	root := t.TempDir()
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}
	oldPath := filepath.Join(metaDir, "old.json")
	newPath := filepath.Join(metaDir, "new.json")
	writeFile(t, oldPath, `{}`)
	writeFile(t, newPath, `{}`)
	oldTime := time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatalf("chtimes new: %v", err)
	}

	files, err := (Store{SessionMetaDir: metaDir}).ListSessionMetaFiles()
	if err != nil {
		t.Fatalf("ListSessionMetaFiles returned error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("file count = %d, want 2", len(files))
	}
	if filepath.Base(files[0].Path) != "new.json" || filepath.Base(files[1].Path) != "old.json" {
		t.Fatalf("files order = %#v, want newest first", files)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestSanitizeMetaJSON_GivenValidJSON_WhenSanitized_ThenReturnsBytesUnchanged(t *testing.T) {
	input := []byte(`{"session_id":"abc","duration_minutes":3}`)
	got := SanitizeMetaJSON(input)
	if string(got) != string(input) {
		t.Fatalf("SanitizeMetaJSON altered valid JSON: got %q, want %q", got, input)
	}
	if !json.Valid(got) {
		t.Fatal("SanitizeMetaJSON returned invalid JSON for valid input")
	}
}

func TestSanitizeMetaJSON_GivenNullPaddedJSON_WhenSanitized_ThenStripsNullBytes(t *testing.T) {
	base := `{"session_id":"abc123","duration_minutes":5}`
	input := []byte(base + "\x00\x00\x00\x00\x00")
	got := SanitizeMetaJSON(input)
	if string(got) != base {
		t.Fatalf("SanitizeMetaJSON = %q, want null bytes stripped to %q", got, base)
	}
	if !json.Valid(got) {
		t.Fatal("SanitizeMetaJSON result is not valid JSON")
	}
}

func TestSanitizeMetaJSON_GivenTruncatedMidString_WhenSanitized_ThenRecoversCompletePrecedingFields(t *testing.T) {
	// Truncated mid-value in the second field; the first field should be recovered.
	input := []byte(`{"session_id":"abc123","first_prompt":"hello wor`)
	got := SanitizeMetaJSON(input)
	if !json.Valid(got) {
		t.Fatalf("SanitizeMetaJSON result is not valid JSON: %q", got)
	}
	var meta map[string]any
	if err := json.Unmarshal(got, &meta); err != nil {
		t.Fatalf("unmarshal recovered JSON: %v", err)
	}
	if meta["session_id"] != "abc123" {
		t.Fatalf("session_id = %#v, want \"abc123\"", meta["session_id"])
	}
	// first_prompt was truncated mid-string so it must not appear in recovered output.
	if _, ok := meta["first_prompt"]; ok {
		t.Fatal("first_prompt unexpectedly present after mid-string truncation recovery")
	}
}

func TestSanitizeMetaJSON_GivenTruncatedMissingClosingBrace_WhenSanitized_ThenRecoversCompletePrecedingFields(t *testing.T) {
	// Truncated after a complete numeric value, missing only the closing '}'.
	// The repair should close the brace and include all complete key-value pairs.
	input := []byte(`{"session_id":"abc123","duration_minutes":5`)
	got := SanitizeMetaJSON(input)
	if !json.Valid(got) {
		t.Fatalf("SanitizeMetaJSON result is not valid JSON: %q", got)
	}
	var meta map[string]any
	if err := json.Unmarshal(got, &meta); err != nil {
		t.Fatalf("unmarshal recovered JSON: %v", err)
	}
	if meta["session_id"] != "abc123" {
		t.Fatalf("session_id = %#v, want \"abc123\"", meta["session_id"])
	}
	if dur, ok := meta["duration_minutes"].(float64); !ok || dur != 5 {
		t.Fatalf("duration_minutes = %#v, want 5", meta["duration_minutes"])
	}
}

func TestSanitizeMetaJSON_GivenAllNullBytes_WhenSanitized_ThenReturnsEmptySlice(t *testing.T) {
	input := []byte("\x00\x00\x00\x00")
	got := SanitizeMetaJSON(input)
	if len(got) != 0 {
		t.Fatalf("SanitizeMetaJSON = %q, want empty slice", got)
	}
}

func TestSanitizeMetaJSON_GivenAllNullBytes_WhenLoadSessionMeta_ThenReturnsParseError(t *testing.T) {
	// Guards against panic: an all-null file must produce a parse error, not crash.
	root := t.TempDir()
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}
	sid := "12345678-1234-1234-1234-123456789abc"
	if err := os.WriteFile(filepath.Join(metaDir, sid+".json"), []byte("\x00\x00\x00\x00"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := (Store{SessionMetaDir: metaDir}).LoadSessionMeta(sid)
	if err == nil {
		t.Fatal("LoadSessionMeta returned nil error for all-null file, want parse error")
	}
	if !strings.Contains(err.Error(), "parse session meta") {
		t.Fatalf("error = %v, want parse session meta", err)
	}
}

func TestSanitizeMetaJSON_GivenNullPaddedFile_WhenLoadSessionMeta_ThenParsesFieldsCorrectly(t *testing.T) {
	// Integration: null-padded file on disk must parse cleanly through LoadSessionMeta.
	root := t.TempDir()
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}
	sid := "12345678-1234-1234-1234-123456789abc"
	base := `{"session_id":"` + sid + `","duration_minutes":7,"first_prompt":"hello"}`
	padded := []byte(base)
	padded = append(padded, make([]byte, 128)...) // 128 null bytes of padding
	if err := os.WriteFile(filepath.Join(metaDir, sid+".json"), padded, 0o644); err != nil {
		t.Fatalf("write padded file: %v", err)
	}

	meta, err := (Store{SessionMetaDir: metaDir}).LoadSessionMeta(sid)
	if err != nil {
		t.Fatalf("LoadSessionMeta returned error for null-padded file: %v", err)
	}
	if meta["session_id"] != sid {
		t.Fatalf("session_id = %#v, want %q", meta["session_id"], sid)
	}
	if meta["first_prompt"] != "hello" {
		t.Fatalf("first_prompt = %#v, want \"hello\"", meta["first_prompt"])
	}
}

func TestSanitizeMetaJSON_GivenTruncatedFile_WhenLoadSessionMeta_ThenRecoverableFieldsArePresent(t *testing.T) {
	// Integration: a truncated file must still return the fields that appeared
	// before the truncation point when those fields form complete key-value pairs.
	root := t.TempDir()
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}
	sid := "12345678-1234-1234-1234-123456789abc"
	// Truncated after "duration_minutes":10 — missing the closing '}'.
	truncated := `{"session_id":"` + sid + `","duration_minutes":10`
	writeFile(t, filepath.Join(metaDir, sid+".json"), truncated)

	meta, err := (Store{SessionMetaDir: metaDir}).LoadSessionMeta(sid)
	if err != nil {
		t.Fatalf("LoadSessionMeta returned error for truncated-but-recoverable file: %v", err)
	}
	if meta["session_id"] != sid {
		t.Fatalf("session_id = %#v, want %q", meta["session_id"], sid)
	}
}

func TestScanTranscriptHeaders_GivenValidJSONL_ThenExtractsHeaderInfo(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "projects", "-Users-me-myproject")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	sid := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	transcript := `{"type":"user","timestamp":"2026-06-15T10:00:00+00:00","message":{"role":"user","content":"what is the capital of France?"}}` + "\n"
	writeFile(t, filepath.Join(projectDir, sid+".jsonl"), transcript)

	store := Store{ProjectsDir: filepath.Join(root, "projects"), HeaderScanner: claudecodec.Codec{}}
	entries := store.ScanTranscriptHeaders()

	if len(entries) != 1 {
		t.Fatalf("ScanTranscriptHeaders returned %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.SessionID != sid {
		t.Fatalf("SessionID = %q, want %q", e.SessionID, sid)
	}
	if e.StartTime != "2026-06-15T10:00:00+00:00" {
		t.Fatalf("StartTime = %q, want %q", e.StartTime, "2026-06-15T10:00:00+00:00")
	}
	if e.FirstPrompt != "what is the capital of France?" {
		t.Fatalf("FirstPrompt = %q, want %q", e.FirstPrompt, "what is the capital of France?")
	}
	if e.FromMeta {
		t.Fatal("FromMeta = true, want false for JSONL-scanned entry")
	}
	if e.ProjectPath != "-Users-me-myproject" {
		t.Fatalf("ProjectPath = %q, want %q", e.ProjectPath, "-Users-me-myproject")
	}
}

func TestScanTranscriptHeaders_GivenNoUserMessage_ThenFirstPromptIsEmpty(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "projects", "-Users-me-proj")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	sid := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	transcript := `{"type":"assistant","timestamp":"2026-06-15T10:00:00+00:00","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}` + "\n"
	writeFile(t, filepath.Join(projectDir, sid+".jsonl"), transcript)

	store := Store{ProjectsDir: filepath.Join(root, "projects"), HeaderScanner: claudecodec.Codec{}}
	entries := store.ScanTranscriptHeaders()

	if len(entries) != 1 {
		t.Fatalf("ScanTranscriptHeaders returned %d entries, want 1", len(entries))
	}
	if entries[0].FirstPrompt != "" {
		t.Fatalf("FirstPrompt = %q, want empty string (no user message)", entries[0].FirstPrompt)
	}
}

func TestScanTranscriptHeaders_GivenCommandMessage_ThenSkipsToNextUserMessage(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "projects", "-Users-me-proj")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	sid := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	lines := []string{
		`{"type":"user","timestamp":"2026-06-15T10:00:00+00:00","message":{"role":"user","content":"<command-name>/qa</command-name>"}}`,
		`{"type":"user","timestamp":"2026-06-15T10:00:01+00:00","message":{"role":"user","content":"explain Go interfaces"}}`,
		"",
	}
	writeFile(t, filepath.Join(projectDir, sid+".jsonl"), strings.Join(lines, "\n"))

	store := Store{ProjectsDir: filepath.Join(root, "projects"), HeaderScanner: claudecodec.Codec{}}
	entries := store.ScanTranscriptHeaders()

	if len(entries) != 1 {
		t.Fatalf("ScanTranscriptHeaders returned %d entries, want 1", len(entries))
	}
	if entries[0].FirstPrompt != "explain Go interfaces" {
		t.Fatalf("FirstPrompt = %q, want %q (command message skipped)", entries[0].FirstPrompt, "explain Go interfaces")
	}
}

func TestScanTranscriptHeaders_GivenDuplicateUUID_ThenDeduplicates(t *testing.T) {
	root := t.TempDir()
	projA := filepath.Join(root, "projects", "-Users-me-alpha")
	projB := filepath.Join(root, "projects", "-Users-me-beta")
	if err := os.MkdirAll(projA, 0o755); err != nil {
		t.Fatalf("create projA: %v", err)
	}
	if err := os.MkdirAll(projB, 0o755); err != nil {
		t.Fatalf("create projB: %v", err)
	}
	sid := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	transcript := `{"type":"user","timestamp":"2026-06-15T10:00:00+00:00","message":{"role":"user","content":"hello"}}` + "\n"
	writeFile(t, filepath.Join(projA, sid+".jsonl"), transcript)
	writeFile(t, filepath.Join(projB, sid+".jsonl"), transcript)

	store := Store{ProjectsDir: filepath.Join(root, "projects"), HeaderScanner: claudecodec.Codec{}}
	entries := store.ScanTranscriptHeaders()

	if len(entries) != 1 {
		t.Fatalf("ScanTranscriptHeaders returned %d entries, want 1 (duplicate UUID deduped)", len(entries))
	}
	if entries[0].SessionID != sid {
		t.Fatalf("SessionID = %q, want %q", entries[0].SessionID, sid)
	}
}

func TestListAllSessions_GivenMetaAndJSONL_ThenMergesWithMetaPriority(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "projects", "-Users-me-proj")
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}
	sid := "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"

	// JSONL transcript with different first prompt than meta
	transcript := `{"type":"user","timestamp":"2026-06-15T10:00:00+00:00","message":{"role":"user","content":"jsonl prompt"}}` + "\n"
	writeFile(t, filepath.Join(projectDir, sid+".jsonl"), transcript)

	// Metadata with canonical values
	writeFile(t, filepath.Join(metaDir, sid+".json"),
		`{"session_id":"`+sid+`","project_path":"/Users/me/proj","start_time":"2026-06-15T10:00:00+00:00","duration_minutes":5,"user_message_count":3,"assistant_message_count":4,"first_prompt":"meta prompt"}`)

	store := Store{ProjectsDir: filepath.Join(root, "projects"), SessionMetaDir: metaDir, HeaderScanner: claudecodec.Codec{}}
	entries, warnings := store.ListAllSessions()

	if len(warnings) != 0 {
		t.Fatalf("ListAllSessions returned warnings: %v", warnings)
	}
	if len(entries) != 1 {
		t.Fatalf("ListAllSessions returned %d entries, want 1 (no duplicate)", len(entries))
	}
	e := entries[0]
	if !e.FromMeta {
		t.Fatal("FromMeta = false, want true (metadata must win over JSONL)")
	}
	if e.FirstPrompt != "meta prompt" {
		t.Fatalf("FirstPrompt = %q, want %q (meta values take priority)", e.FirstPrompt, "meta prompt")
	}
	if e.DurationMinutes != 5 {
		t.Fatalf("DurationMinutes = %d, want 5", e.DurationMinutes)
	}
}

func TestListAllSessions_GivenJSONLOnly_ThenIncludesInResults(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "projects", "-Users-me-proj")
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}
	sid := "ffffffff-ffff-ffff-ffff-ffffffffffff"
	transcript := `{"type":"user","timestamp":"2026-06-15T11:00:00+00:00","message":{"role":"user","content":"help me with Go"}}` + "\n"
	writeFile(t, filepath.Join(projectDir, sid+".jsonl"), transcript)
	// No metadata file for this session

	store := Store{ProjectsDir: filepath.Join(root, "projects"), SessionMetaDir: metaDir, HeaderScanner: claudecodec.Codec{}}
	entries, warnings := store.ListAllSessions()

	if len(warnings) != 0 {
		t.Fatalf("ListAllSessions returned warnings: %v", warnings)
	}
	if len(entries) != 1 {
		t.Fatalf("ListAllSessions returned %d entries, want 1 (JSONL-only session included)", len(entries))
	}
	e := entries[0]
	if e.SessionID != sid {
		t.Fatalf("SessionID = %q, want %q", e.SessionID, sid)
	}
	if e.FromMeta {
		t.Fatal("FromMeta = true, want false for JSONL-only session")
	}
	if e.FirstPrompt != "help me with Go" {
		t.Fatalf("FirstPrompt = %q, want %q", e.FirstPrompt, "help me with Go")
	}
}

func TestListAllSessions_GivenBothSources_ThenSortsByStartTimeDesc(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "projects", "-Users-me-proj")
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}

	// Older session from metadata
	sidOld := "11111111-1111-1111-1111-111111111111"
	writeFile(t, filepath.Join(metaDir, sidOld+".json"),
		`{"session_id":"`+sidOld+`","project_path":"/Users/me/proj","start_time":"2026-06-01T09:00:00+00:00","first_prompt":"old session"}`)

	// Newer session from JSONL only
	sidNew := "22222222-2222-2222-2222-222222222222"
	transcript := `{"type":"user","timestamp":"2026-06-15T09:00:00+00:00","message":{"role":"user","content":"new session"}}` + "\n"
	writeFile(t, filepath.Join(projectDir, sidNew+".jsonl"), transcript)

	store := Store{ProjectsDir: filepath.Join(root, "projects"), SessionMetaDir: metaDir, HeaderScanner: claudecodec.Codec{}}
	entries, _ := store.ListAllSessions()

	if len(entries) != 2 {
		t.Fatalf("ListAllSessions returned %d entries, want 2", len(entries))
	}
	// Newer session (sidNew) must come first
	if entries[0].SessionID != sidNew {
		t.Fatalf("entries[0].SessionID = %q, want %q (newer session first)", entries[0].SessionID, sidNew)
	}
	if entries[1].SessionID != sidOld {
		t.Fatalf("entries[1].SessionID = %q, want %q (older session second)", entries[1].SessionID, sidOld)
	}
}
