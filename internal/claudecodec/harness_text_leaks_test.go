package claudecodec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression: the invocation notice Claude Code writes after a /design command became the list preview of the session (v0.1.84)
func TestScanHeader_GivenInvocationNoticeBeforeTheFirstQuestion_WhenScanned_ThenPreviewIsTheQuestion(t *testing.T) {
	tests := map[string]struct {
		lines         []string
		wantFirstText string
	}{
		"a notice after the command injection is skipped": {
			lines: []string{
				`{"type":"user","timestamp":"2026-11-02T02:44:46Z","message":{"role":"user","content":"<command-message>design</command-message>\n<command-name>/design</command-name>\n<command-args>landing page</command-args>"}}`,
				`{"type":"user","timestamp":"2026-11-02T02:44:46Z","isMeta":true,"turnCompanion":true,"message":{"role":"user","content":[{"type":"text","text":"` +
					"`/design` was invoked: a request for a design made as a NEW Artifact.\\n\\nThe brief:\\n\\nlanding page" + `"}]}}`,
				`{"type":"user","timestamp":"2026-11-02T02:50:00Z","message":{"role":"user","content":"make the header sticky"}}`,
			},
			wantFirstText: "make the header sticky",
		},
		"a notice with no question after it leaves the command preview": {
			lines: []string{
				`{"type":"user","timestamp":"2026-11-02T02:44:46Z","message":{"role":"user","content":"<command-message>design</command-message>\n<command-name>/design</command-name>"}}`,
				`{"type":"user","timestamp":"2026-11-02T02:44:46Z","isMeta":true,"turnCompanion":true,"message":{"role":"user","content":[{"type":"text","text":"` +
					"`/design` was invoked: a request for a design made as a NEW Artifact." + `"}]}}`,
			},
			wantFirstText: "[/design]",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			if err := os.WriteFile(path, []byte(strings.Join(tc.lines, "\n")+"\n"), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			info, err := (Codec{}).ScanHeader(path)
			if err != nil {
				t.Fatalf("ScanHeader returned error: %v", err)
			}
			if info.FirstUserPrompt != tc.wantFirstText {
				t.Errorf("FirstUserPrompt = %q, want %q", info.FirstUserPrompt, tc.wantFirstText)
			}
		})
	}
}
