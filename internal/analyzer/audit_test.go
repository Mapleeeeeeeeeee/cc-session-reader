package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

// TestComputeAudit_ToolResultCutUsesRuneSafeTruncation guards that a sample
// straddling a multi-byte rune boundary at the truncation cap is truncated on
// a rune boundary, not a byte boundary (which would either panic or split a
// rune in half).
func TestComputeAudit_ToolResultCutUsesRuneSafeTruncation(t *testing.T) {
	text := strings.Repeat("a", 299) + "你"
	events := []session.Event{
		{
			Kind: session.EventToolResult,
			Tool: &session.ToolResult{Success: true, RawName: "Bash", Text: text},
		},
	}

	result := ComputeAudit(events)
	items := result.Samples[BucketSuccessBash]
	if len(items) != 1 {
		t.Fatalf("success_output_bash count = %d, want 1", len(items))
	}
	if !utf8.ValidString(items[0]) {
		t.Fatalf("audit sample is not valid UTF-8: %q", items[0])
	}
	if !strings.Contains(items[0], "你") {
		t.Fatalf("audit sample should keep the boundary rune intact, got %q", items[0])
	}
}

// TestComputeAudit_GivenFailedBashResult_ThenBucketedAsFailureOutputWithCutChars
// pins the failure_output bucket: cutChars is raw text length minus the
// literal Summary() the render pipeline keeps (session.ToolResult.Summary()'s
// format is already pinned by TestToolResultSummary in event_test.go, so it is
// hardcoded here as the "kept" reference rather than re-derived from the SUT).
func TestComputeAudit_GivenFailedBashResult_ThenBucketedAsFailureOutputWithCutChars(t *testing.T) {
	text := "boom\n" + strings.Repeat("z", 300)
	const wantKept = " -> FAILED: boom" // first line "boom", not noise, well under the 200-rune excerpt budget

	events := []session.Event{
		{
			Kind: session.EventToolResult,
			Tool: &session.ToolResult{Success: false, RawName: "Bash", Text: text},
		},
	}

	result := ComputeAudit(events)

	wantCut := utf8.RuneCountInString(text) - utf8.RuneCountInString(wantKept)
	if got := result.BucketChars[BucketFailureOutput]; got != wantCut {
		t.Fatalf("BucketChars[failure_output] = %d, want %d", got, wantCut)
	}
	items := result.Samples[BucketFailureOutput]
	if len(items) != 1 || !strings.HasPrefix(items[0], "[Bash] boom") {
		t.Fatalf("Samples[failure_output] = %#v, want one item prefixed \"[Bash] boom\"", items)
	}
	// A failed result must never leak into a success_output_* bucket.
	for _, bucket := range []string{BucketSuccessReadFile, BucketSuccessBash, BucketSuccessAgent, BucketSuccessOther} {
		if len(result.Samples[bucket]) != 0 {
			t.Fatalf("Samples[%s] = %#v, want empty for a failed result", bucket, result.Samples[bucket])
		}
	}
}

// TestComputeAudit_GivenSuccessfulToolResults_ThenBucketedByToolFamily pins
// the success_output_* family split: Read/Write/Edit/Agent collapse to the
// bare "-> ok" excerpt (ADR-002's suppression, reused via Summary()), so they
// lose more of their raw text than Bash/other tools which keep a first-line
// excerpt.
func TestComputeAudit_GivenSuccessfulToolResults_ThenBucketedByToolFamily(t *testing.T) {
	const text = "result line\n" // + filler below
	const firstLine = "result line"
	filler := strings.Repeat("y", 200)
	fullText := text + filler
	rawChars := utf8.RuneCountInString(fullText)

	tests := []struct {
		name       string
		toolName   string
		wantBucket string
		wantKept   string
	}{
		{"Read collapses to bare ok", session.ToolRead, BucketSuccessReadFile, " -> ok"},
		{"Write collapses to bare ok", session.ToolWrite, BucketSuccessReadFile, " -> ok"},
		{"Edit collapses to bare ok", session.ToolEdit, BucketSuccessReadFile, " -> ok"},
		{"Agent collapses to bare ok", session.ToolAgent, BucketSuccessAgent, " -> ok"},
		{"Bash keeps a first-line excerpt", session.ToolBash, BucketSuccessBash, " -> ok: " + firstLine},
		{"Grep (other) keeps a first-line excerpt", "Grep", BucketSuccessOther, " -> ok: " + firstLine},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := []session.Event{
				{
					Kind: session.EventToolResult,
					Tool: &session.ToolResult{Success: true, RawName: tt.toolName, Text: fullText},
				},
			}

			result := ComputeAudit(events)

			wantCut := rawChars - utf8.RuneCountInString(tt.wantKept)
			if got := result.BucketChars[tt.wantBucket]; got != wantCut {
				t.Fatalf("BucketChars[%s] = %d, want %d", tt.wantBucket, got, wantCut)
			}
			if len(result.Samples[tt.wantBucket]) != 1 {
				t.Fatalf("Samples[%s] = %#v, want exactly one sample", tt.wantBucket, result.Samples[tt.wantBucket])
			}
		})
	}
}

// TestComputeAudit_GivenTrivialToolResult_ThenNotSampled guards the
// cutChars<=0 skip: a result so short that Summary()'s own status wording
// outweighs it carries nothing CUT, so it must not pollute the histogram or
// sample list.
func TestComputeAudit_GivenTrivialToolResult_ThenNotSampled(t *testing.T) {
	events := []session.Event{
		{
			Kind: session.EventToolResult,
			Tool: &session.ToolResult{Success: true, RawName: session.ToolBash, Text: "ok"},
		},
	}

	result := ComputeAudit(events)

	for _, bucket := range BucketOrder {
		if got := result.BucketChars[bucket]; got != 0 {
			t.Fatalf("BucketChars[%s] = %d, want 0 for a trivial result", bucket, got)
		}
		if len(result.Samples[bucket]) != 0 {
			t.Fatalf("Samples[%s] = %#v, want empty for a trivial result", bucket, result.Samples[bucket])
		}
	}
}

// TestComputeAudit_TracksToolInputCutInFull verifies that a tool_use's raw
// input JSON is entirely CUT (the render pipeline only ever shows a derived
// one-line description, never the JSON itself, so there is no "kept" portion
// to subtract — unlike tool results, which keep a Summary() excerpt).
func TestComputeAudit_TracksToolInputCutInFull(t *testing.T) {
	input := session.ToolInput{Raw: map[string]any{"command": "ls -la", "description": "List files"}}
	events := []session.Event{
		{
			Kind: session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{
				ToolUses: []session.ToolUse{{ID: "t1", Name: "Bash", Input: input}},
			},
		},
	}

	result := ComputeAudit(events)

	want := utf8.RuneCountInString(input.MarshalNoEscape())
	if got := result.BucketChars[BucketToolInput]; got != want {
		t.Fatalf("BucketChars[tool_input] = %d, want %d", got, want)
	}
	items := result.Samples[BucketToolInput]
	if len(items) != 1 || !strings.HasPrefix(items[0], "[Bash]") {
		t.Fatalf("Samples[tool_input] = %#v, want one item prefixed \"[Bash]\"", items)
	}
}

// TestComputeAudit_TracksThinkingCutInFull verifies thinking blocks are
// entirely CUT: the default render pipeline never surfaces them (VerboseThinking
// is off), so the full block length counts, matching ComputeStats' own
// omission of thinking from its raw/filtered accounting.
func TestComputeAudit_TracksThinkingCutInFull(t *testing.T) {
	events := []session.Event{
		{
			Kind:      session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{Thinking: []string{"private reasoning about the plan"}},
		},
	}

	result := ComputeAudit(events)

	want := utf8.RuneCountInString("private reasoning about the plan")
	if got := result.BucketChars[BucketThinking]; got != want {
		t.Fatalf("BucketChars[thinking] = %d, want %d", got, want)
	}
	if len(result.Samples[BucketThinking]) != 1 {
		t.Fatalf("Samples[thinking] = %#v, want one item", result.Samples[BucketThinking])
	}
}

// TestComputeAudit_BucketsHarnessNoiseSources pins every user-message subtype
// the render pipeline treats as safe-to-drop framework boilerplate (system
// reminders, context-usage tables, command injections, teammate warnings,
// and machine command output) as harness_noise, and confirms skill
// injections are excluded — they carry real user-requested SKILL.md content,
// not framework noise, so lumping them in here would mislabel them.
func TestComputeAudit_BucketsHarnessNoiseSources(t *testing.T) {
	tests := []struct {
		name string
		user *session.UserMessage
		want bool // true: classified as harness_noise
	}{
		{"system reminder", &session.UserMessage{IsSystemReminder: true, Text: "reminder body"}, true},
		{"context usage table", &session.UserMessage{IsContextUsage: true, Text: "context table"}, true},
		{"command injection", &session.UserMessage{IsCommandInjection: true, Text: "/cc-session read abc"}, true},
		{"teammate warning", &session.UserMessage{IsTeammateMessage: true, Text: "teammate said hi"}, true},
		{"command noise", &session.UserMessage{IsCommandNoise: true, Text: "bash stdout dump"}, true},
		{"skill injection excluded", &session.UserMessage{IsSkillInjection: true, SkillName: "cc-session", Text: "skill body"}, false},
		{"plain user text excluded", &session.UserMessage{Text: "hello there"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := []session.Event{{Kind: session.EventUserMessage, User: tt.user}}
			result := ComputeAudit(events)

			got := len(result.Samples[BucketHarnessNoise]) == 1
			if got != tt.want {
				t.Fatalf("classified as harness_noise = %v, want %v (samples: %#v)", got, tt.want, result.Samples[BucketHarnessNoise])
			}
			if tt.want {
				want := utf8.RuneCountInString(tt.user.Text)
				if got := result.BucketChars[BucketHarnessNoise]; got != want {
					t.Fatalf("BucketChars[harness_noise] = %d, want %d", got, want)
				}
			}
		})
	}
}

// TestComputeAudit_CategorizesSystemNoise verifies EventNoise entries (system,
// file-history-snapshot, etc.) are bucketed as harness_noise in full, matching
// ComputeStats' own "this is machine boilerplate" treatment of the same
// events.
func TestComputeAudit_CategorizesSystemNoise(t *testing.T) {
	events := []session.Event{
		{
			Kind:    session.EventNoise,
			RawType: "system",
			Noise:   &session.NoiseEvent{Text: "system details"},
		},
	}

	result := ComputeAudit(events)
	if got := len(result.Samples[BucketHarnessNoise]); got != 1 {
		t.Fatalf("Samples[harness_noise] count = %d, want 1", got)
	}
	want := utf8.RuneCountInString("system details")
	if got := result.BucketChars[BucketHarnessNoise]; got != want {
		t.Fatalf("BucketChars[harness_noise] = %d, want %d", got, want)
	}
}

// TestComputeAudit_GivenNoFailures_ThenFailureStatsZero pins the explicit
// "no failures in this session" case for the failure-length distribution:
// zero failed tool results must report a zero-value FailureLengthStats, not
// a division-by-zero panic or garbage percentile.
func TestComputeAudit_GivenNoFailures_ThenFailureStatsZero(t *testing.T) {
	events := []session.Event{
		{Kind: session.EventToolResult, Tool: &session.ToolResult{Success: true, RawName: "Bash", Text: "all good"}},
	}

	result := ComputeAudit(events)

	if result.Failures != (FailureLengthStats{}) {
		t.Fatalf("Failures = %#v, want zero-value FailureLengthStats", result.Failures)
	}
}

// TestComputeAudit_GivenMultipleFailures_ThenComputesMedianP90Max pins the
// nearest-rank percentile computation against a hand-checkable set of five
// known lengths: median is the classic middle value of an odd-sized sorted
// set, and p90 of 5 elements lands on the last (5th) element under
// nearest-rank (ceil(0.9*5)=5).
func TestComputeAudit_GivenMultipleFailures_ThenComputesMedianP90Max(t *testing.T) {
	lengths := []int{40, 10, 100, 30, 20} // sorted: 10, 20, 30, 40, 100
	events := make([]session.Event, 0, len(lengths))
	for i, n := range lengths {
		events = append(events, session.Event{
			Kind: session.EventToolResult,
			Tool: &session.ToolResult{
				Success:   false,
				RawName:   "Bash",
				ToolUseID: fmt.Sprintf("t%d", i),
				Text:      strings.Repeat("e", n),
			},
		})
	}

	result := ComputeAudit(events)

	want := FailureLengthStats{Count: 5, Median: 30, P90: 100, Max: 100}
	if result.Failures != want {
		t.Fatalf("Failures = %#v, want %#v", result.Failures, want)
	}
}

// TestReadAll_GivenSuccessfulReadResultWithoutCommandName_ThenAuditBucketsByResolvedToolName
// is a regression guard mirroring claudecodec's own
// TestReadAll_GivenReadToolResultWithoutCommandName_ThenBareOkSuppressionApplies:
// real Claude Code transcripts carry no commandName on a Read's toolUseResult
// at all — RawName only resolves via the preceding tool_use's tool_use_id. A
// fixture with RawName hand-filled (as every other test in this file does)
// cannot catch a regression in that resolution path, so this one runs the
// real two-entry JSONL sequence through claudecodec.ReadAll.
func TestReadAll_GivenSuccessfulReadResultWithoutCommandName_ThenAuditBucketsByResolvedToolName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	// content is embedded directly into hand-built JSON string literals below,
	// so newlines must be the literal two-character JSON escape `\n` (raw
	// string, not an interpreted "\n") rather than an actual newline byte —
	// an unescaped newline inside a JSON string is invalid JSON.
	content := strings.Repeat(`line of file content\n`, 20)
	lines := []string{
		`{"type":"assistant","timestamp":"2026-07-15T00:00:00Z","message":{"role":"assistant","content":[` +
			`{"type":"tool_use","id":"toolu_read1","name":"Read","input":{"file_path":"/repo/README.md"}}` +
			`]}}`,
		`{"type":"user","timestamp":"2026-07-15T00:00:01Z","toolUseResult":{` +
			`"type":"text","file":{"content":"` + content + `","numLines":20}` +
			`},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_read1",` +
			`"content":"     1\t` + content + `"}]}}`,
		"",
	}
	writeFixture(t, path, lines)

	events, err := claudecodec.ReadAll(path)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}

	result := ComputeAudit(events)

	if got := len(result.Samples[BucketSuccessReadFile]); got != 1 {
		t.Fatalf("Samples[success_output_read_file] count = %d, want 1 (RawName must resolve via the preceding tool_use)", got)
	}
	for _, bucket := range []string{BucketSuccessBash, BucketSuccessAgent, BucketSuccessOther, BucketFailureOutput} {
		if got := len(result.Samples[bucket]); got != 0 {
			t.Fatalf("Samples[%s] = %d items, want 0 — a resolved Read result must not fall through to \"other\"", bucket, got)
		}
	}
}

func writeFixture(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
