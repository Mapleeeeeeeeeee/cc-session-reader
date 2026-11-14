package main

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
)

const (
	estimateTestOverhead = 40_000
	// maxEstimateError is the largest relative error -no-api may have against the
	// count_tokens API; the worst of 40 real sessions was 8.7%.
	maxEstimateError = 0.10
)

const estimateTestUsage = `{"input_tokens":99000,"cache_creation_input_tokens":1000,"cache_creation":{"ephemeral_5m_input_tokens":1000,"ephemeral_1h_input_tokens":0},"cache_read_input_tokens":0,"output_tokens":1000}`

type estimateTurn struct {
	user      string
	assistant string
}

// writeConversationSession stores a session whose turns carry the given texts.
func writeConversationSession(t *testing.T, root string, sid string, turns []estimateTurn) {
	t.Helper()
	projectDir := filepath.Join(root, "projects", "proj")
	metaDir := filepath.Join(root, "session-meta")
	for _, dir := range []string{projectDir, metaDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create dir: %v", err)
		}
	}
	var lines []string
	for i, turn := range turns {
		userLine, err := json.Marshal(map[string]any{
			"type":      "user",
			"timestamp": "2026-05-28T00:00:" + twoDigits(2*i) + "Z",
			"message":   map[string]any{"role": "user", "content": turn.user},
		})
		if err != nil {
			t.Fatalf("marshal user line: %v", err)
		}
		assistantLine, err := json.Marshal(map[string]any{
			"type":      "assistant",
			"timestamp": "2026-05-28T00:00:" + twoDigits(2*i+1) + "Z",
			"message": map[string]any{
				"role":    "assistant",
				"content": turn.assistant,
				"usage":   json.RawMessage(estimateTestUsage),
			},
		})
		if err != nil {
			t.Fatalf("marshal assistant line: %v", err)
		}
		lines = append(lines, string(userLine), string(assistantLine))
	}
	transcript := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	writeListMeta(t, metaDir, sid, "/tmp/proj", turns[0].user)
}

func twoDigits(n int) string {
	return strconv.Itoa(n/10) + strconv.Itoa(n%10)
}

func repeatedTurns(count int, user string, assistant string) []estimateTurn {
	turns := make([]estimateTurn, count)
	for i := range turns {
		turns[i] = estimateTurn{user: user, assistant: assistant}
	}
	return turns
}

// estimateCases pair fixture sessions with measuredFilteredTokens, the count
// count_tokens (claude-opus-5-5) returned for each session's FilteredText.
var estimateCases = []struct {
	name                   string
	turns                  []estimateTurn
	measuredFilteredTokens int
}{
	{
		name: "chinese heavy session",
		turns: repeatedTurns(8,
			"請幫我檢查這個專案的登入流程，確認使用者輸入錯誤密碼三次之後帳號會被暫時鎖定，並且把鎖定時間改成可以在設定檔裡調整。",
			"我已經讀過登入相關的程式碼，目前錯誤次數只存在記憶體裡，伺服器重新啟動之後就會歸零。建議把錯誤次數和鎖定到期時間寫進資料庫，並且在設定檔新增鎖定分鐘數這個欄位，預設為十五分鐘。修改完成後我會補上單元測試，涵蓋連續輸入錯誤、鎖定期間登入、鎖定到期之後恢復登入三種情況。"),
		measuredFilteredTokens: 2124,
	},
	{
		name: "technical english session",
		turns: repeatedTurns(8,
			"Fix the failing test in internal/auth/lockout_test.go: TestLockout_GivenThreeBadPasswords_ThenAccountLocked returns nil instead of ErrAccountLocked.",
			"The counter in internal/auth/lockout.go is a map[string]int guarded by sync.Mutex and is reset by Server.Restart(). Moving it to the users table (columns failed_attempts INT NOT NULL DEFAULT 0, locked_until TIMESTAMPTZ) and reading LOCKOUT_MINUTES=15 from config/settings.json fixes it; run `go test ./internal/auth/... -run TestLockout -count=1` to confirm."),
		measuredFilteredTokens: 2004,
	},
}

// Regression: -no-api estimated tokens as characters / 2, 26% to 39% below count_tokens on sessions with a lot of Chinese (v0.1.84)
func TestRunBenchmark_GivenNoAPIFlag_WhenEstimatingFilteredTokens_ThenStaysCloseToTokenCountingAPI(t *testing.T) {
	for _, tc := range estimateCases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			const sid = "44444444-4444-4444-4444-444444444444"
			writeConversationSession(t, root, sid, tc.turns)

			var stdout, stderr bytes.Buffer
			store := parser.Store{ProjectsDir: filepath.Join(root, "projects"), SessionMetaDir: filepath.Join(root, "session-meta")}
			args := []string{"--no-api", "--n", "1", "--min-kb", "0", "--overhead", strconv.Itoa(estimateTestOverhead)}
			if err := runBenchmark(args, &stdout, &stderr, store, testReader); err != nil {
				t.Fatalf("runBenchmark returned error: %v", err)
			}

			row := outputLineContaining(stdout.String(), "44444444")
			fields := strings.Fields(row)
			if len(fields) < 3 {
				t.Fatalf("unexpected benchmark row %q in output:\n%s", row, stdout.String())
			}
			newContext, err := strconv.Atoi(strings.ReplaceAll(fields[2], ",", ""))
			if err != nil {
				t.Fatalf("parse NewCtx %q: %v", fields[2], err)
			}

			estimated := newContext - estimateTestOverhead
			relativeError := float64(estimated-tc.measuredFilteredTokens) / float64(tc.measuredFilteredTokens)
			if math.Abs(relativeError) > maxEstimateError {
				t.Fatalf("estimated %d filtered tokens, count_tokens measured %d (%+.1f%%, allowed ±%.0f%%)",
					estimated, tc.measuredFilteredTokens, relativeError*100, maxEstimateError*100)
			}
		})
	}
}
