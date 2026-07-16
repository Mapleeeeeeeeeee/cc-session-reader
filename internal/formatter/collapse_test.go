package formatter

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
)

// bashAttempt describes one Bash tool_use/tool_result pair for the retry-loop
// collapse fixtures below.
type bashAttempt struct {
	toolID      string
	command     string
	description string
	success     bool
	resultText  string
}

// writeBashAttemptsFixture writes a real transcript (assistant tool_use
// followed by its user tool_result, per attempt) and runs it through the real
// parse pipeline via FormatRead(claudecodec.Codec{}) — never hand-filling
// RawName, per this project's own regression history (claudecodec's RawName
// only resolves through the preceding tool_use's tool_use_id, so a
// hand-built session.ToolResult can't catch a regression there).
func writeBashAttemptsFixture(t *testing.T, attempts []bashAttempt) string {
	t.Helper()

	root := t.TempDir()
	transcriptPath := filepath.Join(root, formatterFixtureSessionID+".jsonl")

	var b strings.Builder
	second := 0
	for _, a := range attempts {
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":"2026-05-28T00:00:%02dZ","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":%q,"input":{"command":%q,"description":%q}}]}}`+"\n",
			second, a.toolID, a.command, a.description)
		second++
		fmt.Fprintf(&b, `{"type":"user","timestamp":"2026-05-28T00:00:%02dZ","toolUseResult":{"success":%t,"commandName":"Bash"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":%q}]}}`+"\n",
			second, a.success, a.toolID, a.resultText)
		second++
	}
	if err := os.WriteFile(transcriptPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return transcriptPath
}

// writeBashAttemptsWithNarrationFixture is writeBashAttemptsFixture but
// inserts a plain assistant-text message ("narration") between the two
// attempt groups, mirroring the model narrating between retries — this
// flushes the pending tool list, so it must break any retry-loop grouping.
func writeBashAttemptsWithNarrationFixture(t *testing.T, before, after []bashAttempt, narration string) string {
	t.Helper()

	root := t.TempDir()
	transcriptPath := filepath.Join(root, formatterFixtureSessionID+".jsonl")

	writeAttempt := func(b *strings.Builder, second *int, a bashAttempt) {
		fmt.Fprintf(b, `{"type":"assistant","timestamp":"2026-05-28T00:00:%02dZ","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":%q,"input":{"command":%q,"description":%q}}]}}`+"\n",
			*second, a.toolID, a.command, a.description)
		*second++
		fmt.Fprintf(b, `{"type":"user","timestamp":"2026-05-28T00:00:%02dZ","toolUseResult":{"success":%t,"commandName":"Bash"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":%q}]}}`+"\n",
			*second, a.success, a.toolID, a.resultText)
		*second++
	}

	var b strings.Builder
	second := 0
	for _, a := range before {
		writeAttempt(&b, &second, a)
	}
	fmt.Fprintf(&b, `{"type":"assistant","timestamp":"2026-05-28T00:00:%02dZ","message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`+"\n",
		second, narration)
	second++
	for _, a := range after {
		writeAttempt(&b, &second, a)
	}

	if err := os.WriteFile(transcriptPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return transcriptPath
}

func TestFormatRead_GivenConsecutiveFailedRetriesWithSameCommandAndSameError_ThenCollapsesIntoFailedCountLine(t *testing.T) {
	transcriptPath := writeBashAttemptsFixture(t, []bashAttempt{
		{toolID: "retry-tool-1", command: "npm test", description: "Run tests", success: false, resultText: "Error: connection refused"},
		{toolID: "retry-tool-2", command: "npm test", description: "Run tests", success: false, resultText: "Error: connection refused"},
		{toolID: "retry-tool-3", command: "npm test", description: "Run tests", success: false, resultText: "Error: connection refused"},
	})

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}
	got := out.String()

	want := "[Bash#ol-3] Run tests -> FAILED ×3: Error: connection refused"
	if !strings.Contains(got, want) {
		t.Fatalf("expected collapsed retry-loop line\nwant substring: %q\ngot:\n%s", want, got)
	}
	if strings.Count(got, "[Bash#") != 1 {
		t.Fatalf("expected exactly one Bash summary line after collapsing, got:\n%s", got)
	}
}

func TestFormatRead_GivenConsecutiveFailedRetriesWithSameCommandButDifferentErrors_ThenDoesNotCollapse(t *testing.T) {
	// Regression guard: collapseRetryLoops used to key only on tool name +
	// command signature + failure status, so three attempts of the same
	// command failing with three *different* errors collapsed into one
	// "FAILED ×3" line carrying only the last attempt's error — silently
	// hiding the first two. That violates ADR-005's invariant that failure
	// information is never lost, so attempts whose error excerpts differ
	// must stay split even when the command matches.
	transcriptPath := writeBashAttemptsFixture(t, []bashAttempt{
		{toolID: "retry-tool-1", command: "npm test", description: "Run tests", success: false, resultText: "Error: 2 tests failed"},
		{toolID: "retry-tool-2", command: "npm test", description: "Run tests", success: false, resultText: "Error: 3 tests failed"},
		{toolID: "retry-tool-3", command: "npm test", description: "Run tests", success: false, resultText: "Error: TypeError: cannot read prop"},
	})

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "×") {
		t.Fatalf("attempts with different error excerpts must not collapse, got:\n%s", got)
	}
	if strings.Count(got, "[Bash#") != 3 {
		t.Fatalf("expected all three attempts to render individually, got:\n%s", got)
	}
	for _, want := range []string{"Error: 2 tests failed", "Error: 3 tests failed", "Error: TypeError: cannot read prop"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected every distinct error to remain visible, missing %q, got:\n%s", want, got)
		}
	}
}

func TestFormatRead_GivenSingleFailedBashCall_ThenDoesNotShowMultiplier(t *testing.T) {
	// A run of exactly one failed attempt must render as a normal FAILED line —
	// "×1" would be a meaningless label for something that wasn't retried.
	transcriptPath := writeBashAttemptsFixture(t, []bashAttempt{
		{toolID: "single-tool-1", command: "npm test", description: "Run tests", success: false, resultText: "Error: 1 test failed"},
	})

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}
	got := out.String()

	want := "[Bash#ol-1] Run tests -> FAILED: Error: 1 test failed"
	if !strings.Contains(got, want) {
		t.Fatalf("expected uncollapsed single failure line\nwant substring: %q\ngot:\n%s", want, got)
	}
	if strings.Contains(got, "×") {
		t.Fatalf("a single failed call must not show a retry multiplier, got:\n%s", got)
	}
}

func TestFormatRead_GivenRetryLoopInterruptedByAssistantText_ThenDoesNotCollapseAcrossText(t *testing.T) {
	// The model narrating between two otherwise-identical failed attempts
	// flushes the pending tool list, splitting what would otherwise be a
	// 2-attempt retry loop into two independent single attempts — each of
	// which stays uncollapsed (their individual failure text might be what
	// the narration is referring to).
	before := []bashAttempt{
		{toolID: "gap-tool-1", command: "npm test", description: "Run tests", success: false, resultText: "Error: connection refused"},
	}
	after := []bashAttempt{
		{toolID: "gap-tool-2", command: "npm test", description: "Run tests", success: false, resultText: "Error: connection refused"},
	}
	transcriptPath := writeBashAttemptsWithNarrationFixture(t, before, after, "let me check the server logs")

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "×") {
		t.Fatalf("attempts split by assistant narration must not collapse, got:\n%s", got)
	}
	if strings.Count(got, "[Bash#") != 2 {
		t.Fatalf("expected both attempts to render as separate Bash lines, got:\n%s", got)
	}
	if !strings.Contains(got, "let me check the server logs") {
		t.Fatalf("narration text between attempts must still render, got:\n%s", got)
	}
}

func TestFormatRead_GivenMixedSuccessAndFailureSameCommand_ThenDoesNotCollapse(t *testing.T) {
	// A success sandwiched between two failures of the same command is not a
	// clean retry loop — collapsing must not jump over the success to merge
	// the two failed ends together.
	transcriptPath := writeBashAttemptsFixture(t, []bashAttempt{
		{toolID: "mixed-tool-1", command: "npm test", description: "Run tests", success: false, resultText: "Error: flaky failure"},
		{toolID: "mixed-tool-2", command: "npm test", description: "Run tests", success: true, resultText: "All tests passed"},
		{toolID: "mixed-tool-3", command: "npm test", description: "Run tests", success: false, resultText: "Error: different failure"},
	})

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "×") {
		t.Fatalf("a success interrupting the run must prevent collapsing, got:\n%s", got)
	}
	if strings.Count(got, "[Bash#") != 3 {
		t.Fatalf("expected all three attempts to render individually, got:\n%s", got)
	}
}

func TestFormatRead_GivenCommandSharesWordPrefixWithoutTokenBoundary_ThenDoesNotCollapse(t *testing.T) {
	// Regression guard: sameRetryCommand used a bare strings.HasPrefix, so
	// "git add" matched as a prefix of "git add-on-something" even though
	// "add" and "add-on-something" are different tokens — a false-positive
	// retry match. A prefix only counts when the longer command's next
	// character after the shorter one is a token boundary (whitespace).
	transcriptPath := writeBashAttemptsFixture(t, []bashAttempt{
		{toolID: "prefix-tool-1", command: "git add", description: "Stage file", success: false, resultText: "Error: pathspec 'a' did not match any files"},
		{toolID: "prefix-tool-2", command: "git add-on-something", description: "Stage file", success: false, resultText: "Error: pathspec 'a' did not match any files"},
	})

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "×") {
		t.Fatalf("commands sharing a word prefix without a token boundary must not collapse, got:\n%s", got)
	}
	if strings.Count(got, "[Bash#") != 2 {
		t.Fatalf("expected both attempts to render individually, got:\n%s", got)
	}
}

func TestFormatRead_GivenBareCommandFollowedByLongerCommand_ThenDoesNotCollapse(t *testing.T) {
	// Regression guard: when the first attempt's command is a bare word
	// (e.g. "git"), the old prefix check let it absorb any later command
	// starting with that word ("git add", "git commit", ...) into the same
	// retry group, even though they are unrelated calls.
	transcriptPath := writeBashAttemptsFixture(t, []bashAttempt{
		{toolID: "bare-tool-1", command: "git", description: "Check status", success: false, resultText: "Error: not a git repository"},
		{toolID: "bare-tool-2", command: "git add", description: "Stage file", success: false, resultText: "Error: not a git repository"},
	})

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "×") {
		t.Fatalf("a bare command must not absorb a longer, unrelated command, got:\n%s", got)
	}
	if strings.Count(got, "[Bash#") != 2 {
		t.Fatalf("expected both attempts to render individually, got:\n%s", got)
	}
}

func TestFormatRead_GivenRetryCommandExtendedWithTrailingArgs_ThenStillCollapses(t *testing.T) {
	// A legitimate prefix retry — same base command, an extra trailing
	// argument, separated by a token boundary — must keep collapsing. This
	// guards against the token-boundary fix above becoming so strict it
	// breaks the "only the trailing argument differs" case the prefix match
	// was originally added for.
	transcriptPath := writeBashAttemptsFixture(t, []bashAttempt{
		{toolID: "extend-tool-1", command: "npm test", description: "Run tests", success: false, resultText: "Error: connection refused"},
		{toolID: "extend-tool-2", command: "npm test -- --seed=42", description: "Run tests", success: false, resultText: "Error: connection refused"},
	})

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}
	got := out.String()

	want := "[Bash#ol-2] Run tests -> FAILED ×2: Error: connection refused"
	if !strings.Contains(got, want) {
		t.Fatalf("expected collapsed retry-loop line\nwant substring: %q\ngot:\n%s", want, got)
	}
	if strings.Count(got, "[Bash#") != 1 {
		t.Fatalf("expected exactly one Bash summary line after collapsing, got:\n%s", got)
	}
}

func TestFormatRead_GivenVerboseBashAndRetryLoop_ThenSkipsCollapse(t *testing.T) {
	// Mirrors the existing -verbose-bash exemption for collapseCCSessionTools:
	// full Bash output must be preserved per attempt, not discarded by
	// retry-loop collapsing.
	transcriptPath := writeBashAttemptsFixture(t, []bashAttempt{
		{toolID: "verbose-tool-1", command: "npm test", description: "Run tests", success: false, resultText: "Error: attempt one detail"},
		{toolID: "verbose-tool-2", command: "npm test", description: "Run tests", success: false, resultText: "Error: attempt two detail"},
		{toolID: "verbose-tool-3", command: "npm test", description: "Run tests", success: false, resultText: "Error: attempt three detail"},
	})

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{VerboseBash: true}, &out, claudecodec.Codec{}); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "×") {
		t.Fatalf("verbose-bash must skip retry-loop collapsing, got:\n%s", got)
	}
	for _, want := range []string{"Error: attempt one detail", "Error: attempt two detail", "Error: attempt three detail"} {
		if !strings.Contains(got, want) {
			t.Fatalf("verbose-bash output missing full detail %q, got:\n%s", want, got)
		}
	}
}

// readAttempt describes one Read tool_use/tool_result pair for the
// same-file-read collapse fixtures below.
type readAttempt struct {
	toolID     string
	path       string
	offset     int
	limit      int
	hasWindow  bool
	success    bool
	resultText string
}

func writeReadTranscript(t *testing.T, lines []string) string {
	t.Helper()
	root := t.TempDir()
	transcriptPath := filepath.Join(root, formatterFixtureSessionID+".jsonl")
	if err := os.WriteFile(transcriptPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return transcriptPath
}

func readAttemptLines(second *int, a readAttempt) []string {
	input := fmt.Sprintf("%q", a.path)
	if a.hasWindow {
		input = fmt.Sprintf(`%q,"offset":%d,"limit":%d`, a.path, a.offset, a.limit)
	}
	assistantLine := fmt.Sprintf(`{"type":"assistant","timestamp":"2026-05-28T00:00:%02dZ","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","id":%q,"input":{"file_path":%s}}]}}`,
		*second, a.toolID, input)
	*second++
	resultLine := fmt.Sprintf(`{"type":"user","timestamp":"2026-05-28T00:00:%02dZ","toolUseResult":{"success":%t,"commandName":"Read"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":%q}]}}`,
		*second, a.success, a.toolID, a.resultText)
	*second++
	return []string{assistantLine, resultLine}
}

func TestFormatRead_GivenConsecutiveReadsOfSameFile_ThenCollapsesIntoReadCountLine(t *testing.T) {
	second := 0
	var lines []string
	lines = append(lines, readAttemptLines(&second, readAttempt{toolID: "read-tool-1", path: "/repo/src/main.go", success: true, resultText: "line 1\nline 2"})...)
	lines = append(lines, readAttemptLines(&second, readAttempt{toolID: "read-tool-2", path: "/repo/src/main.go", offset: 100, limit: 50, hasWindow: true, success: true, resultText: "line 101\nline 102"})...)
	lines = append(lines, readAttemptLines(&second, readAttempt{toolID: "read-tool-3", path: "/repo/src/main.go", offset: 200, limit: 50, hasWindow: true, success: true, resultText: "line 201\nline 202"})...)
	transcriptPath := writeReadTranscript(t, lines)

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}
	got := out.String()

	want := "[Read#ol-3 ×3] src/main.go -> ok"
	if !strings.Contains(got, want) {
		t.Fatalf("expected collapsed same-file-read line\nwant substring: %q\ngot:\n%s", want, got)
	}
	if strings.Count(got, "[Read#") != 1 {
		t.Fatalf("expected exactly one Read summary line after collapsing, got:\n%s", got)
	}
}

func TestFormatRead_GivenSingleReadCall_ThenDoesNotShowMultiplier(t *testing.T) {
	second := 0
	lines := readAttemptLines(&second, readAttempt{toolID: "solo-read-1", path: "/repo/src/main.go", success: true, resultText: "line 1\nline 2"})
	transcriptPath := writeReadTranscript(t, lines)

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "×") {
		t.Fatalf("a single Read call must not show a multiplier, got:\n%s", got)
	}
	if !strings.Contains(got, "[Read#ad-1]") {
		t.Fatalf("expected uncollapsed single Read line, got:\n%s", got)
	}
}

func TestFormatRead_GivenReadsInterruptedByOtherTool_ThenDoesNotCollapse(t *testing.T) {
	second := 0
	var lines []string
	lines = append(lines, readAttemptLines(&second, readAttempt{toolID: "gap-read-1", path: "/repo/src/main.go", success: true, resultText: "line 1"})...)
	lines = append(lines,
		fmt.Sprintf(`{"type":"assistant","timestamp":"2026-05-28T00:00:%02dZ","message":{"role":"assistant","content":[{"type":"tool_use","name":"Grep","id":"grep-tool-1","input":{"pattern":"TODO"}}]}}`, second),
	)
	second++
	lines = append(lines,
		fmt.Sprintf(`{"type":"user","timestamp":"2026-05-28T00:00:%02dZ","toolUseResult":{"success":true,"commandName":"Grep"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"grep-tool-1","content":"no matches"}]}}`, second),
	)
	second++
	lines = append(lines, readAttemptLines(&second, readAttempt{toolID: "gap-read-2", path: "/repo/src/main.go", success: true, resultText: "line 2"})...)
	transcriptPath := writeReadTranscript(t, lines)

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "×") {
		t.Fatalf("reads split by another tool call must not collapse, got:\n%s", got)
	}
	if strings.Count(got, "[Read#") != 2 {
		t.Fatalf("expected both reads to render as separate lines, got:\n%s", got)
	}
}

func TestFormatRead_GivenReadsWithOneFailure_ThenDoesNotCollapse(t *testing.T) {
	second := 0
	var lines []string
	lines = append(lines, readAttemptLines(&second, readAttempt{toolID: "flaky-read-1", path: "/repo/src/main.go", success: true, resultText: "line 1"})...)
	lines = append(lines, readAttemptLines(&second, readAttempt{toolID: "flaky-read-2", path: "/repo/src/main.go", success: false, resultText: "permission denied"})...)
	lines = append(lines, readAttemptLines(&second, readAttempt{toolID: "flaky-read-3", path: "/repo/src/main.go", success: true, resultText: "line 3"})...)
	transcriptPath := writeReadTranscript(t, lines)

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "×") {
		t.Fatalf("a failed read in the run must prevent collapsing, got:\n%s", got)
	}
	if strings.Count(got, "[Read#") != 3 {
		t.Fatalf("expected all three reads to render individually, got:\n%s", got)
	}
}
