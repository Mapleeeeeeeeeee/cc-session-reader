package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
)

const (
	advisorSessionID  = "12345678-1234-1234-1234-123456789abc"
	advisorToolUseID  = "srvtoolu_01KARK2yp3QcbSLuQghCuhWL"
	advisorShortID    = "uhWL"
	advisorCipherText = "Eo8mCioIFBgCIiRkMzRmNTMxMi03NGE2LTQ4OGMtYjJkYS05N2UzMDY0NTlhZDQ"
)

const (
	advisorCallRecord = `{"type":"assistant","timestamp":"2026-10-13T15:41:52Z","message":{"id":"msg_1","role":"assistant","content":[{"type":"server_tool_use","id":"` + advisorToolUseID + `","name":"advisor","input":{}}]}}`
	advisorFollowUp   = `{"type":"assistant","timestamp":"2026-10-13T15:43:40Z","message":{"id":"msg_1","role":"assistant","content":[{"type":"text","text":"Plan agreed, starting."}]}}`

	advisorRedactedReply = `{"type":"advisor_redacted_result","encrypted_content":"` + advisorCipherText + `"}`
)

func advisorReplyRecord(content string) string {
	return `{"type":"assistant","timestamp":"2026-10-13T15:43:34Z","message":{"id":"msg_1","role":"assistant","content":[{"type":"advisor_tool_result","tool_use_id":"` + advisorToolUseID + `","content":` + content + `}]}}`
}

func storeWithAdvisorTranscript(t *testing.T, lines ...string) parser.Store {
	t.Helper()
	projectsDir := filepath.Join(t.TempDir(), "projects")
	projectDir := filepath.Join(projectsDir, "proj")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	transcript := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(projectDir, advisorSessionID+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return parser.Store{ProjectsDir: projectsDir}
}

func Test_Read_Given_AdvisorConsultation_When_Read_Then_ShowsOneToolLineWithOutcome(t *testing.T) {
	tests := []struct {
		name  string
		reply string // empty means the transcript has no reply record
		want  string
	}{
		{
			name:  "encrypted reply says it is unreadable",
			reply: advisorReplyRecord(advisorRedactedReply),
			want:  "  [advisor#" + advisorShortID + "] -> response is encrypted in the transcript\n",
		},
		{
			name:  "error reply shows FAILED with the error code",
			reply: advisorReplyRecord(`{"type":"advisor_tool_result_error","error_code":"too_many_requests"}`),
			want:  "  [advisor#" + advisorShortID + "] -> FAILED: too_many_requests\n",
		},
		{
			name:  "call without a reply shows the bare call",
			reply: "",
			want:  "  [advisor#" + advisorShortID + "]\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := []string{advisorCallRecord}
			if tt.reply != "" {
				lines = append(lines, tt.reply)
			}
			lines = append(lines, advisorFollowUp)
			store := storeWithAdvisorTranscript(t, lines...)

			var stdout, stderr bytes.Buffer
			if err := runRead([]string{advisorSessionID, "-max-lines", "0"}, &stdout, &stderr, store, testReader); err != nil {
				t.Fatalf("runRead returned error: %v", err)
			}
			got := stdout.String()
			if !strings.Contains(got, tt.want) {
				t.Errorf("read output missing %q\ngot:\n%s", tt.want, got)
			}
			if strings.Contains(got, advisorCipherText) {
				t.Errorf("read output must not contain the encrypted payload\ngot:\n%s", got)
			}
		})
	}
}

func Test_Expand_Given_AdvisorConsultation_When_Expanded_Then_ShowsInputAndReplyStatus(t *testing.T) {
	tests := []struct {
		name  string
		reply string
		want  []string
	}{
		{
			name:  "encrypted reply",
			reply: advisorReplyRecord(advisorRedactedReply),
			want:  []string{"=== [advisor#" + advisorShortID + "] ===", "Result (ok):\nresponse is encrypted in the transcript"},
		},
		{
			name:  "error reply",
			reply: advisorReplyRecord(`{"type":"advisor_tool_result_error","error_code":"execution_time_exceeded"}`),
			want:  []string{"=== [advisor#" + advisorShortID + "] ===", "Result (FAILED):\nexecution_time_exceeded"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := storeWithAdvisorTranscript(t, advisorCallRecord, tt.reply, advisorFollowUp)

			var stdout, stderr bytes.Buffer
			if err := runExpand([]string{advisorSessionID, advisorShortID}, &stdout, &stderr, store, testReader); err != nil {
				t.Fatalf("runExpand returned error: %v", err)
			}
			got := stdout.String()
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("expand output missing %q\ngot:\n%s", want, got)
				}
			}
			if strings.Contains(got, advisorCipherText) {
				t.Errorf("expand output must not contain the encrypted payload\ngot:\n%s", got)
			}
		})
	}
}

func Test_Stats_Given_AdvisorConsultation_When_Counted_Then_PerToolShowsOneAdvisorCall(t *testing.T) {
	store := storeWithAdvisorTranscript(t, advisorCallRecord, advisorReplyRecord(advisorRedactedReply), advisorFollowUp)

	var stdout, stderr bytes.Buffer
	if err := runStats([]string{"--no-tokens", advisorSessionID}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runStats returned error: %v", err)
	}
	advisorRow := regexp.MustCompile(`(?m)^\s*advisor\s+1 calls\b`)
	if !advisorRow.MatchString(stdout.String()) {
		t.Errorf("per-tool stats missing a single advisor call\ngot:\n%s", stdout.String())
	}
}
