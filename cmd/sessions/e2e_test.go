package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_WhenSessionExists_ThenListReadContextAndAuditWorkEndToEnd(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "sessions")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}

	sid := "12345678-1234-1234-1234-123456789abc"
	writeE2EFixture(t, root, sid)

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "list shows the session metadata",
			args: []string{"list", "-n", "1"},
			want: []string{
				sid,
				"05-28 00:00",
				"proj",
				"hello",
			},
		},
		{
			name: "read shows dialogue and tool summary with short ID",
			args: []string{"read", sid},
			want: []string{
				"[05-28 00:00] user:\nhello",
				"[05-28 00:00] assistant:\nhi",
				"[Bash#ol-1] Echo ok -> ok: ok",
			},
		},
		{
			name: "context shows compact session format with short ID",
			args: []string{"context", sid},
			want: []string{
				"# Session 12345678 | proj | 1m",
				"U: hello",
				"A: hi",
				"[Bash#ol-1] Echo ok -> ok: ok",
			},
		},
		{
			name: "audit samples cut tool results",
			args: []string{"audit", sid, "-n", "5"},
			want: []string{
				"=== tool_result_cut (1 items, showing 1) ===",
			},
		},
		{
			name: "stats reports character breakdown without token API",
			args: []string{"stats", "--no-tokens", sid},
			want: []string{
				"Session: 12345678",
				"=== Characters ===",
				"=== Breakdown ===",
				"KEPT  user text:",
				"CUT   tool result (raw):",
			},
		},
		{
			name: "expand shows full tool input and result for short ID",
			args: []string{"expand", sid, "ol-1"},
			want: []string{
				"=== [Bash#ol-1] ===",
				"Input:",
				"echo ok",
				"Result (ok):",
				"ok",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(bin, tt.args...)
			cmd.Env = append(os.Environ(), "HOME="+root)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s failed: %v\n%s", strings.Join(tt.args, " "), err, out)
			}
			got := string(out)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Fatalf("output missing %q:\n%s", want, got)
				}
			}
		})
	}
}

// The cmdX wrappers funnel every error through a single contract: print
// "Error: <msg>" to stderr and exit non-zero. This is the user's only signal
// that a command failed. Tested out-of-process because the contract lives in
// os.Exit, which can't be observed in-process. Mutation guard: if any wrapper
// regresses to a bare Fprintln(err) (no "Error:" prefix) or stops exiting
// non-zero, this turns red.
func TestCLI_WhenSubcommandFails_ThenPrintsErrorPrefixAndExitsNonZero(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "sessions")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}

	// HOME points at an empty root: no transcripts exist, so resolving any
	// session ID fails. One case per distinct wrapper is enough — they share
	// the same wrapper shape.
	tests := []struct {
		name string
		args []string
	}{
		{name: "read with unknown session", args: []string{"read", "no-such-session-id"}},
		{name: "context with unknown session", args: []string{"context", "no-such-session-id"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(bin, tt.args...)
			cmd.Env = append(os.Environ(), "HOME="+root)
			out, err := cmd.CombinedOutput()

			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("expected non-zero exit, got err=%v\noutput:\n%s", err, out)
			}
			if exitErr.ExitCode() == 0 {
				t.Fatalf("expected non-zero exit code, got 0\noutput:\n%s", out)
			}
			if !strings.Contains(string(out), "Error:") {
				t.Fatalf("output missing %q prefix:\n%s", "Error:", out)
			}
		})
	}
}

func writeE2EFixture(t *testing.T, root string, sid string) {
	t.Helper()

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
		`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hi"},{"type":"tool_use","name":"Bash","id":"tool-1","input":{"command":"echo ok","description":"Echo ok"}}]}}`,
		`{"type":"user","timestamp":"2026-05-28T00:00:02Z","toolUseResult":{"success":true,"commandName":"Bash"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}]}}`,
		`{"type":"user","timestamp":"2026-05-28T00:00:03Z","toolUseResult":{"success":true,"commandName":"Bash"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-2","content":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	meta := `{"session_id":"` + sid + `","project_path":"/tmp/proj","duration_minutes":1,"user_message_count":1,"assistant_message_count":1,"first_prompt":"hello","start_time":"2026-05-28T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(metaDir, sid+".json"), []byte(meta), 0o644); err != nil {
		t.Fatalf("write session meta: %v", err)
	}
}
