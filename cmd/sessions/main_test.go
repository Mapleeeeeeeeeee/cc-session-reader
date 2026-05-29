package main

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/analyzer"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/tokens"
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
		{name: "read verbose bash long", flag: "--verbose-bash"},
		{name: "read verbose bash short", flag: "-verbose-bash"},
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
	want := []string{"no-tokens", "verbose-agents", "verbose-bash", "verbose-thinking", "verbose-commands"}
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

// Regression: the old formatNumber negated negatives via -n, which overflows
// for math.MinInt (-MinInt == MinInt) and recursed forever. It must terminate
// and group the digits without panicking. Expected value is hand-derived from
// strconv.Itoa(math.MinInt) with thousands separators inserted.
func TestFormatNumber_GivenMinInt_ThenGroupsWithoutOverflow(t *testing.T) {
	digits := strconv.Itoa(math.MinInt)[1:] // strip leading '-'
	var sb strings.Builder
	sb.WriteByte('-')
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(c)
	}
	want := sb.String()

	if got := formatNumber(math.MinInt); got != want {
		t.Fatalf("formatNumber(math.MinInt) = %q, want %q", got, want)
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

// When the Anthropic API is unreachable (no API key), the two concurrent
// CountTokensAPI calls both fail and runStats must fall back to the local
// heuristic estimate. This guards the fallback branch — the only token path
// exercised by the existing suite uses --no-tokens, which skips it entirely.
// Offline and deterministic: clearing ANTHROPIC_API_KEY makes both calls error.
func TestRunStats_WhenTokenAPIUnavailable_ThenFallsBackToEstimate(t *testing.T) {
	// Fixture has a tool_use whose raw input/result is CUT from the filtered
	// stream, so RawText strictly exceeds FilteredText. This makes the raw >
	// filtered invariant non-trivial: a SUT mutation that swaps the two streams
	// turns the assertion red (which it would not with an empty-tool fixture
	// where the streams are equal).
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
		`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hi"},{"type":"tool_use","name":"Bash","id":"toolu_1","input":{"command":"echo this raw input is cut from the filtered stream"}}]}}`,
		`{"type":"user","timestamp":"2026-05-28T00:00:02Z","toolUseResult":{"success":true,"commandName":"Bash"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"and this raw result is also cut from the filtered stream"}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	writeListMeta(t, metaDir, sid, "/tmp/proj", "hello")
	store := parser.Store{ProjectsDir: filepath.Join(root, ".claude", "projects"), SessionMetaDir: metaDir}

	// Empty key => CountTokensAPI returns an error before any network call,
	// so both goroutines fail and runStats takes the estimate branch.
	t.Setenv("ANTHROPIC_API_KEY", "")

	var stdout, stderr bytes.Buffer
	// No --no-tokens: we want the token-counting path to actually run.
	if err := runStats([]string{sid}, &stdout, &stderr, store); err != nil {
		t.Fatalf("runStats returned error: %v", err)
	}
	got := stdout.String()

	// Proves we took the fallback branch, not the API branch nor --no-tokens.
	if !strings.Contains(got, "=== Tokens (estimated) ===") {
		t.Fatalf("stdout missing estimated-tokens header:\n%s", got)
	}
	if strings.Contains(got, "=== Tokens (Anthropic API) ===") {
		t.Fatalf("stdout unexpectedly took the API branch:\n%s", got)
	}
	// Both estimates print with the '~' approximate marker.
	if strings.Count(got, "~") < 2 {
		t.Fatalf("stdout missing '~' markers on raw and filtered estimates:\n%s", got)
	}

	// Re-derive both estimates from the analyzer output (the same source
	// runStats uses) rather than transcribing runStats' printed numbers. The
	// fixture cuts real tool content, so RawText differs from FilteredText and
	// the two estimates come out different — which is what lets the line-routing
	// assertions below distinguish a stream swap.
	//
	// Note: we deliberately do NOT assert raw >= filtered. EstimateTokens is not
	// monotonic with content size (filtering replaces raw tool JSON with a
	// human-readable summary that can be longer for short inputs), so any such
	// ordering would be a false invariant rather than a real guarantee.
	result := analyzer.ComputeStats(mustReadAll(t, filepath.Join(projectDir, sid+".jsonl")))
	rawEst := tokens.EstimateTokens(result.RawText)
	filtEst := tokens.EstimateTokens(result.FilteredText)
	if rawEst == filtEst {
		t.Fatalf("fixture too weak: raw and filtered estimates both %d, a stream swap would be undetectable", rawEst)
	}
	// The raw estimate must land on the "Raw:" line and the filtered estimate on
	// the "Filtered:" line. A SUT mutation that swaps the two streams moves each
	// number onto the wrong labelled line and turns these red.
	if !strings.Contains(got, "Raw:      "+pad10(formatNumber(rawEst))+" ~") {
		t.Fatalf("stdout missing raw estimate %s on Raw line:\n%s", formatNumber(rawEst), got)
	}
	if !strings.Contains(got, "Filtered: "+pad10(formatNumber(filtEst))+" ~") {
		t.Fatalf("stdout missing filtered estimate %s on Filtered line:\n%s", formatNumber(filtEst), got)
	}
}

// pad10 right-aligns s in a 10-wide field, matching runStats' "%10s" format,
// so assertions can pin a value to a specific labelled line.
func pad10(s string) string {
	if len(s) >= 10 {
		return s
	}
	return strings.Repeat(" ", 10-len(s)) + s
}

// When both concurrent token-count calls succeed, runStats prints the
// Anthropic-API block (not the estimate block). The package-level countTokensFn
// seam lets us stub a deterministic success offline. Returning distinct raw/
// filtered counts proves each result is routed to its own line rather than one
// value being printed twice.
func TestRunStats_WhenTokenAPISucceeds_ThenPrintsAPITokenCounts(t *testing.T) {
	// A tool_use carries raw input/result that is CUT from the filtered stream,
	// so RawText and FilteredText genuinely differ — letting the stub route a
	// distinct count to each line and proving they aren't conflated.
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
		`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hi"},{"type":"tool_use","name":"Bash","id":"toolu_1","input":{"command":"echo this raw input is cut from filtered"}}]}}`,
		`{"type":"user","timestamp":"2026-05-28T00:00:02Z","toolUseResult":{"success":true,"commandName":"Bash"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"and this raw result is also cut"}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	writeListMeta(t, metaDir, sid, "/tmp/proj", "hello")
	store := parser.Store{ProjectsDir: filepath.Join(root, ".claude", "projects"), SessionMetaDir: metaDir}

	result := analyzer.ComputeStats(mustReadAll(t, filepath.Join(projectDir, sid+".jsonl")))
	if result.RawText == result.FilteredText {
		t.Fatalf("fixture invalid: RawText and FilteredText are identical, cannot distinguish lines")
	}

	const (
		rawCount  = 1234
		filtCount = 567
	)
	original := countTokensFn
	t.Cleanup(func() { countTokensFn = original })
	// Route the larger count to the raw stream, the smaller to the filtered
	// stream. A mutation that fed both lines the same text would print one
	// value twice and drop the other.
	countTokensFn = func(text string) (int, error) {
		if text == result.RawText {
			return rawCount, nil
		}
		return filtCount, nil
	}

	var stdout, stderr bytes.Buffer
	if err := runStats([]string{sid}, &stdout, &stderr, store); err != nil {
		t.Fatalf("runStats returned error: %v", err)
	}
	got := stdout.String()

	if !strings.Contains(got, "=== Tokens (Anthropic API) ===") {
		t.Fatalf("stdout missing API-tokens header:\n%s", got)
	}
	if strings.Contains(got, "=== Tokens (estimated) ===") {
		t.Fatalf("stdout unexpectedly took the estimate branch:\n%s", got)
	}
	if strings.Contains(got, "~") {
		t.Fatalf("API branch should not print '~' approximate markers:\n%s", got)
	}
	// Each count must land on its correctly-labelled line. Pinning the value to
	// the line (not just "appears somewhere") is what catches a SUT mutation
	// that swaps which stream each goroutine counts.
	if !strings.Contains(got, "Raw:      "+pad10(formatNumber(rawCount))+"\n") {
		t.Fatalf("stdout missing raw API count %d on Raw line:\n%s", rawCount, got)
	}
	if !strings.Contains(got, "Filtered: "+pad10(formatNumber(filtCount))+"\n") {
		t.Fatalf("stdout missing filtered API count %d on Filtered line:\n%s", filtCount, got)
	}
	// Saved = raw - filtered must also be printed (guards the saved math).
	if !strings.Contains(got, "Saved:    "+pad10(formatNumber(rawCount-filtCount))) {
		t.Fatalf("stdout missing saved count %d on Saved line:\n%s", rawCount-filtCount, got)
	}
}

func mustReadAll(t *testing.T, path string) []session.Event {
	t.Helper()
	events, err := claudecodec.ReadAll(path)
	if err != nil {
		t.Fatalf("ReadAll(%s): %v", path, err)
	}
	return events
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

func TestRunExpand_GivenExistingToolID_WhenExpanded_ThenShowsFullInputAndResult(t *testing.T) {
	// writeCLIFixture has no tool_use events, so build a fixture with one inline.
	root := t.TempDir()
	sid := "12345678-1234-1234-1234-123456789abc"
	projectDir := filepath.Join(root, ".claude", "projects", "proj")
	metaDir := filepath.Join(root, ".claude", "usage-data", "session-meta")
	_ = os.MkdirAll(projectDir, 0o755)
	_ = os.MkdirAll(metaDir, 0o755)
	transcript := strings.Join([]string{
		`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hi"},{"type":"tool_use","name":"Bash","id":"toolu_01XYZabcdefgABCDuCVa","input":{"command":"echo hello","description":"Say hello"}}]}}`,
		`{"type":"user","timestamp":"2026-05-28T00:00:02Z","toolUseResult":{"success":true,"commandName":"Bash"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01XYZabcdefgABCDuCVa","content":"hello"}]}}`,
		"",
	}, "\n")
	_ = os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644)
	writeListMeta(t, metaDir, sid, "/tmp/proj", "hello")

	var stdout, stderr bytes.Buffer
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, ".claude", "projects"),
		SessionMetaDir: metaDir,
	}
	// Short ID of "toolu_01XYZabcdefgABCDuCVa" -> last 4 chars = "uCVa"
	err := runExpand([]string{sid, "uCVa"}, &stdout, &stderr, store)
	if err != nil {
		t.Fatalf("runExpand returned error: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "=== [Bash#uCVa] ===") {
		t.Fatalf("expand output missing header\ngot:\n%s", got)
	}
	if !strings.Contains(got, "echo hello") {
		t.Fatalf("expand output missing tool input\ngot:\n%s", got)
	}
	if !strings.Contains(got, "Result (ok):") {
		t.Fatalf("expand output missing result status\ngot:\n%s", got)
	}
	if !strings.Contains(got, "hello") {
		t.Fatalf("expand output missing result text\ngot:\n%s", got)
	}
}

// Regression: short IDs are only the last 4 chars of tool_use_id, so two
// distinct tools can share one short ID. The old code keyed a map by short ID
// and silently overwrote, so expand would return whichever tool appeared last
// in the transcript — confidently wrong data with no warning. expand must
// instead detect the collision, refuse to guess, and list the full IDs so the
// user can disambiguate.
func TestRunExpand_GivenShortIDCollision_WhenExpanded_ThenWarnsAndDoesNotGuess(t *testing.T) {
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

	// Two tools whose full IDs differ but whose last 4 chars both equal "uCVa".
	firstID := "toolu_01AAAAAAAAAAAAAAuCVa"
	secondID := "toolu_01BBBBBBBBBBBBBBuCVa"
	transcript := strings.Join([]string{
		`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"` + firstID + `","input":{"command":"echo first"}}]}}`,
		`{"type":"assistant","timestamp":"2026-05-28T00:00:02Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","id":"` + secondID + `","input":{"command":"echo second"}}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	writeListMeta(t, metaDir, sid, "/tmp/proj", "hello")

	var stdout, stderr bytes.Buffer
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, ".claude", "projects"),
		SessionMetaDir: metaDir,
	}
	err := runExpand([]string{sid, "uCVa"}, &stdout, &stderr, store)

	// The only requested ID is ambiguous, so nothing was expanded -> error.
	if err == nil {
		t.Fatal("runExpand returned nil error, want collision to yield no matches")
	}
	// Must not silently emit either tool's body as if it were the answer.
	if strings.Contains(stdout.String(), "echo first") || strings.Contains(stdout.String(), "echo second") {
		t.Fatalf("expand emitted a guessed tool body on collision:\n%s", stdout.String())
	}
	// Must warn about ambiguity and list BOTH full IDs for disambiguation.
	gotErr := stderr.String()
	if !strings.Contains(gotErr, "ambiguous") {
		t.Fatalf("stderr missing ambiguity warning:\n%s", gotErr)
	}
	if !strings.Contains(gotErr, firstID) || !strings.Contains(gotErr, secondID) {
		t.Fatalf("stderr did not list both colliding full IDs:\n%s", gotErr)
	}
}

// A user can disambiguate a colliding short ID by passing the full tool_use_id.
func TestRunExpand_GivenFullIDOnCollision_WhenExpanded_ThenResolvesUnambiguously(t *testing.T) {
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

	firstID := "toolu_01AAAAAAAAAAAAAAuCVa"
	secondID := "toolu_01BBBBBBBBBBBBBBuCVa"
	transcript := strings.Join([]string{
		`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"` + firstID + `","input":{"command":"echo first"}}]}}`,
		`{"type":"assistant","timestamp":"2026-05-28T00:00:02Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","id":"` + secondID + `","input":{"command":"echo second"}}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	writeListMeta(t, metaDir, sid, "/tmp/proj", "hello")

	var stdout, stderr bytes.Buffer
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, ".claude", "projects"),
		SessionMetaDir: metaDir,
	}
	if err := runExpand([]string{sid, secondID}, &stdout, &stderr, store); err != nil {
		t.Fatalf("runExpand returned error: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "echo second") {
		t.Fatalf("full ID did not resolve to the intended tool:\n%s", got)
	}
	if strings.Contains(got, "echo first") {
		t.Fatalf("full ID resolved to the wrong tool:\n%s", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty when full ID is unambiguous", stderr.String())
	}
}

func TestRunExpand_GivenNonexistentToolID_WhenExpanded_ThenReturnsError(t *testing.T) {
	root, sid := writeCLIFixture(t)
	var stdout, stderr bytes.Buffer
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, ".claude", "projects"),
		SessionMetaDir: filepath.Join(root, ".claude", "usage-data", "session-meta"),
	}
	err := runExpand([]string{sid, "ZZZZ"}, &stdout, &stderr, store)
	if err == nil {
		t.Fatal("runExpand should return error when no tool IDs match")
	}
	if !strings.Contains(err.Error(), "no matching tool IDs found") {
		t.Fatalf("error = %v, want 'no matching tool IDs found'", err)
	}
}

func TestRunExpand_GivenNoArgs_WhenCalled_ThenReturnsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runExpand(nil, &stdout, &stderr, parser.Store{})
	if err == nil {
		t.Fatal("runExpand should return error with no args")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("error = %v, want usage message", err)
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
