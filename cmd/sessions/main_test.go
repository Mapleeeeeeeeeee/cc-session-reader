package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
)

func TestSampleCount(t *testing.T) {
	tests := []struct {
		name      string
		requested int
		total     int
		want      int
	}{
		{
			name:      "negative request shows none",
			requested: -1,
			total:     3,
			want:      0,
		},
		{
			name:      "request larger than total is capped",
			requested: 10,
			total:     3,
			want:      3,
		},
		{
			name:      "request within total is unchanged",
			requested: 2,
			total:     3,
			want:      2,
		},
		{
			name:      "zero request shows none",
			requested: 0,
			total:     3,
			want:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sampleCount(tt.requested, tt.total)
			if got != tt.want {
				t.Fatalf("sampleCount(%d, %d) = %d, want %d", tt.requested, tt.total, got, tt.want)
			}
		})
	}
}

func TestReorderArgs_DoesNotConsumeBooleanFlagPositionals(t *testing.T) {
	tests := []struct {
		name string
		flag string
	}{
		{name: "read verbose agents long", flag: "--verbose-agents"},
		{name: "read verbose agents short", flag: "-verbose-agents"},
		{name: "stats no tokens long", flag: "--no-tokens"},
		{name: "stats no tokens short", flag: "-no-tokens"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reorderArgs([]string{tt.flag, "12345678"})
			want := []string{tt.flag, "12345678"}
			if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
				t.Fatalf("reorderArgs() = %#v, want %#v", got, want)
			}
		})
	}
}

func TestReorderBoolFlags_CoversSupportedBooleanFlags(t *testing.T) {
	want := []string{"--no-tokens", "--verbose-agents", "-no-tokens", "-verbose-agents"}
	for _, flag := range want {
		if !reorderBoolFlags[flag] {
			t.Fatalf("reorderBoolFlags missing %s", flag)
		}
	}
	if len(reorderBoolFlags) != len(want) {
		t.Fatalf("reorderBoolFlags has %d entries, want %d", len(reorderBoolFlags), len(want))
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{input: 0, want: "0"},
		{input: 12, want: "12"},
		{input: 999, want: "999"},
		{input: 1000, want: "1,000"},
		{input: 1234567, want: "1,234,567"},
		{input: -1234567, want: "-1,234,567"},
	}

	for _, tt := range tests {
		if got := formatNumber(tt.input); got != tt.want {
			t.Fatalf("formatNumber(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRunList_WhenProjectFilterMatches_ThenOnlyPrintsMatchingProjects(t *testing.T) {
	root := t.TempDir()
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}
	writeListMeta(t, metaDir, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "/tmp/api", "api prompt")
	writeListMeta(t, metaDir, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "/tmp/web", "web prompt")

	var stdout, stderr bytes.Buffer
	err := runList([]string{"-p", "api"}, &stdout, &stderr, parser.Store{SessionMetaDir: metaDir})
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "api prompt") {
		t.Fatalf("stdout missing api session:\n%s", got)
	}
	if strings.Contains(got, "web prompt") {
		t.Fatalf("stdout includes filtered web session:\n%s", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunList_WhenProjectFilterMatchesNothing_ThenWritesNoSessionsFound(t *testing.T) {
	root := t.TempDir()
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}
	writeListMeta(t, metaDir, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "/tmp/api", "api prompt")

	var stdout, stderr bytes.Buffer
	err := runList([]string{"-p", "web"}, &stdout, &stderr, parser.Store{SessionMetaDir: metaDir})
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); got != "No sessions found.\n" {
		t.Fatalf("stderr = %q, want no sessions message", got)
	}
}

func TestRunList_WhenMetadataIsInvalid_ThenWarnsAndContinues(t *testing.T) {
	root := t.TempDir()
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}
	writeListMeta(t, metaDir, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "/tmp/api", "api prompt")
	if err := os.WriteFile(filepath.Join(metaDir, "bad.json"), []byte(`{"session_id":`), 0o644); err != nil {
		t.Fatalf("write bad meta: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := runList(nil, &stdout, &stderr, parser.Store{SessionMetaDir: metaDir})
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "api prompt") {
		t.Fatalf("stdout missing valid session:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "warning: skipping invalid metadata") {
		t.Fatalf("stderr missing invalid metadata warning:\n%s", stderr.String())
	}
}

func TestRunRead_WhenSessionIDIsMissing_ThenReturnsError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runRead(nil, &stdout, &stderr, parser.Store{})
	if err == nil {
		t.Fatal("runRead returned nil error, want missing session_id")
	}
	if !strings.Contains(err.Error(), "session_id is required") {
		t.Fatalf("error = %v, want session_id is required", err)
	}
}

func TestRunRead_WhenSessionExists_ThenWritesOutput(t *testing.T) {
	root, sid := writeCLIFixture(t)
	var stdout, stderr bytes.Buffer
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, ".claude", "projects"),
		SessionMetaDir: filepath.Join(root, ".claude", "usage-data", "session-meta"),
	}

	err := runRead([]string{sid}, &stdout, &stderr, store)
	if err != nil {
		t.Fatalf("runRead returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "[05-28 00:00] user:\nhello") {
		t.Fatalf("stdout missing read output:\n%s", stdout.String())
	}
}

func TestRunContext_WhenSessionExists_ThenWritesHeaderAndContext(t *testing.T) {
	root, sid := writeCLIFixture(t)
	var stdout, stderr bytes.Buffer
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, ".claude", "projects"),
		SessionMetaDir: filepath.Join(root, ".claude", "usage-data", "session-meta"),
	}

	err := runContext([]string{sid}, &stdout, &stderr, store)
	if err != nil {
		t.Fatalf("runContext returned error: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "# Session 12345678 | proj | 1m") || !strings.Contains(got, "U: hello") {
		t.Fatalf("stdout missing context output:\n%s", got)
	}
}

func TestRunStats_WhenNoTokens_ThenWritesCharacterBreakdown(t *testing.T) {
	root, sid := writeCLIFixture(t)
	var stdout, stderr bytes.Buffer
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, ".claude", "projects"),
		SessionMetaDir: filepath.Join(root, ".claude", "usage-data", "session-meta"),
	}

	err := runStats([]string{"--no-tokens", sid}, &stdout, &stderr, store)
	if err != nil {
		t.Fatalf("runStats returned error: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "Session: 12345678") || !strings.Contains(got, "=== Breakdown ===") {
		t.Fatalf("stdout missing stats output:\n%s", got)
	}
}

func TestRunAudit_WhenSamplesIsNegative_ThenShowsZeroSamplesWithoutPanic(t *testing.T) {
	root := t.TempDir()
	sid := "12345678-1234-1234-1234-123456789abc"
	projectDir := filepath.Join(root, ".claude", "projects", "proj")
	metaDir := filepath.Join(root, ".claude", "usage-data", "session-meta")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}

	transcript := `{"type":"user","timestamp":"2026-05-28T00:00:01Z","toolUseResult":{"success":true,"commandName":"Bash"},"message":{"role":"user","content":[{"type":"tool_result","content":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	t.Setenv("HOME", root)

	var stdout, stderr bytes.Buffer
	err := runAudit([]string{sid, "-n", "-1"}, &stdout, &stderr, parser.DefaultStore())
	if err != nil {
		t.Fatalf("runAudit returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "=== tool_result_cut (1 items, showing 0) ===") {
		t.Fatalf("stdout missing zero-sample header:\n%s", out)
	}
	if !strings.Contains(out, "... and 1 more") {
		t.Fatalf("stdout missing remaining-sample count:\n%s", out)
	}
}

func writeCLIFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	sid := "12345678-1234-1234-1234-123456789abc"
	projectDir := filepath.Join(root, ".claude", "projects", "proj")
	metaDir := filepath.Join(root, ".claude", "usage-data", "session-meta")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}
	transcript := strings.Join([]string{
		`{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	writeListMeta(t, metaDir, sid, "/tmp/proj", "hello")
	return root, sid
}

func writeListMeta(t *testing.T, metaDir string, sid string, projectPath string, firstPrompt string) {
	t.Helper()
	meta := `{"session_id":"` + sid + `","project_path":"` + projectPath + `","duration_minutes":1,"user_message_count":1,"assistant_message_count":2,"first_prompt":"` + firstPrompt + `","start_time":"2026-05-28T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(metaDir, sid+".json"), []byte(meta), 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}
}
