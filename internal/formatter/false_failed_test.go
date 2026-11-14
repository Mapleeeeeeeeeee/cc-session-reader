package formatter_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/formatter"
)

// bashCallTranscript is one Bash tool call and its tool_result, with neither
// is_error nor toolUseResult.success on the result, the shape ADR-003 says
// the reader must classify from the text alone.
func bashCallTranscript(t *testing.T, command string, resultText string) string {
	t.Helper()
	encodedResult, err := json.Marshal(resultText)
	if err != nil {
		t.Fatalf("encode result text: %v", err)
	}
	encodedCommand, err := json.Marshal(command)
	if err != nil {
		t.Fatalf("encode command: %v", err)
	}
	return fmt.Sprintf(
		`{"type":"assistant","timestamp":"2026-11-10T01:00:00Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_status01","name":"Bash","input":{"command":%s}}]}}
{"type":"user","timestamp":"2026-11-10T01:00:01Z","toolUseResult":{"stdout":"","stderr":""},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_status01","content":%s}]}}
`, encodedCommand, encodedResult)
}

func readOutputFor(t *testing.T, transcript string) string {
	t.Helper()
	var out bytes.Buffer
	events := readTranscript(t, transcript)
	if err := formatter.FormatReadEvents(events, map[string]bool{}, 0, 0, formatter.FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	return out.String()
}

// Regression: a successful result whose body quoted "Exit code N" or "hook error" was marked FAILED (v0.1.84)
func TestIntegration_Read_GivenBashResult_WhenSignatureAppearsInBody_ThenFailedOnlyIfItOpensTheResult(t *testing.T) {
	tests := map[string]struct {
		resultText string
		wantFailed bool
	}{
		"a paged transcript quoting a hook rejection further down is ok": {
			resultText: "[page 1/2 | lines 1-5 of 9]\n[10:00:00] harness:\nPreToolUse:Bash hook error: blocked by policy\n",
			wantFailed: false,
		},
		"a paged transcript quoting an exit code line further down is ok": {
			resultText: "[page 1/2 | lines 1-5 of 9]\nExit code 1\nsome output\n",
			wantFailed: false,
		},
		"a file read that mentions a hook error in a later line is ok": {
			resultText: "1\t# notes\n2\tthe command printed a hook error once\n",
			wantFailed: false,
		},
		"a non-zero exit code opening the result is FAILED": {
			resultText: "Exit code 2\nboom\n",
			wantFailed: true,
		},
		"a hook rejection opening the result is FAILED": {
			resultText: "PreToolUse:Bash hook error: Refused to remove protected directory\n",
			wantFailed: true,
		},
		"a hook rejection naming the command in brackets is FAILED": {
			resultText: "PreToolUse:Bash [rm -rf /tmp/x] hook error: blocked by policy",
			wantFailed: true,
		},
		"an exit code 0 opening the result is ok": {
			resultText: "Exit code 0\ndone\n",
			wantFailed: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := readOutputFor(t, bashCallTranscript(t, "run-something", tc.resultText))

			if hasFailed := strings.Contains(got, "FAILED"); hasFailed != tc.wantFailed {
				t.Errorf("output contains FAILED = %v, want %v\ngot:\n%s", hasFailed, tc.wantFailed, got)
			}
		})
	}
}

// Regression: a cc-session page whose body quoted "hook error" was marked FAILED, which cleared injectSessionID and cut the collapse of the paged call (v0.1.84)
func TestIntegration_Read_GivenCCSessionInheritPageQuotingHookError_WhenFormatted_ThenCallStillCollapses(t *testing.T) {
	page := "[page 1/2 | lines 1-5 of 9]\n[10:00:00] harness:\nPreToolUse:Bash hook error: blocked by policy\n"

	got := readOutputFor(t, bashCallTranscript(t, "cc-session inherit abcd1234-0000-0000-0000-000000000000", page))

	want := "(cc-session#us01: inherited session abcd1234 here, 9 lines omitted)"
	if !strings.Contains(got, want) {
		t.Errorf("output missing the collapsed cc-session line %q\ngot:\n%s", want, got)
	}
}
