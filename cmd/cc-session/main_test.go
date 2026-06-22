package main

import (
	"bytes"
	"flag"
	"fmt"
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

// testReader is the concrete reader used by all tests.
var testReader = claudecodec.Codec{}

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
	want := []string{"no-tokens", "verbose-agents", "verbose-bash", "verbose-thinking", "verbose-commands", "reset"}
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
		if got := analyzer.FormatNumber(tt.input); got != tt.want {
			t.Fatalf("FormatNumber(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// Regression: the old FormatNumber negated negatives via -n, which overflows
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

	if got := analyzer.FormatNumber(math.MinInt); got != want {
		t.Fatalf("FormatNumber(math.MinInt) = %q, want %q", got, want)
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

// Regression (F1): `list -n -1` (and 0) used to return an empty list with exit
// 0, giving no signal that the requested display count was nonsensical. -n is
// "max sessions to display" and must be a positive integer; any value < 1 is a
// validation error that fails the command.
func TestRunList_WhenDisplayCountIsNotPositive_ThenReturnsValidationError(t *testing.T) {
	tests := []struct {
		name  string
		nFlag string
	}{
		{name: "negative", nFlag: "-1"},
		{name: "zero", nFlag: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			// Store is irrelevant: validation must reject before listing anything.
			err := runList([]string{"-n", tt.nFlag}, &stdout, &stderr, parser.Store{})
			if err == nil {
				t.Fatalf("runList(-n %s) returned nil error, want validation error", tt.nFlag)
			}
			if !strings.Contains(err.Error(), "-n must be a positive integer") {
				t.Fatalf("error = %v, want '-n must be a positive integer'", err)
			}
		})
	}
}

// Regression (F2): `read -max-lines -1` was silently treated as 0 (unlimited),
// hiding the user's mistake. A negative cap is meaningless and must be a
// validation error. 0 retains its documented "unlimited" meaning.
func TestRunRead_WhenMaxLinesIsNegative_ThenReturnsValidationError(t *testing.T) {
	root, sid := writeCLIFixture(t)
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, ".claude", "projects"),
		SessionMetaDir: filepath.Join(root, ".claude", "usage-data", "session-meta"),
	}
	var stdout, stderr bytes.Buffer
	err := runRead([]string{sid, "-max-lines", "-1"}, &stdout, &stderr, store, testReader)
	if err == nil {
		t.Fatal("runRead(-max-lines -1) returned nil error, want validation error")
	}
	if !strings.Contains(err.Error(), "-max-lines must be") {
		t.Fatalf("error = %v, want -max-lines validation message", err)
	}
}

// Guards F2's carve-out: -max-lines 0 is the documented "unlimited" sentinel and
// must keep working. The fixture has two message lines plus headers, all of which
// must appear when no cap is applied.
func TestRunRead_WhenMaxLinesIsZero_ThenEmitsUnlimitedOutput(t *testing.T) {
	root, sid := writeCLIFixture(t)
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, ".claude", "projects"),
		SessionMetaDir: filepath.Join(root, ".claude", "usage-data", "session-meta"),
	}
	var stdout, stderr bytes.Buffer
	if err := runRead([]string{sid, "-max-lines", "0"}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runRead(-max-lines 0) returned error: %v", err)
	}
	got := stdout.String()
	// Both the user line and the assistant reply survive when unlimited.
	if !strings.Contains(got, "hello") || !strings.Contains(got, "hi") {
		t.Fatalf("stdout missing unlimited output (both messages):\n%s", got)
	}
}

// Regression (F3): an empty session_id used to be matched as a prefix against
// every session, producing the misleading "ambiguous prefix ”" error. The user
// simply omitted the ID, so the message must say it is required and must not
// mention ambiguity. ResolveSession is the single choke point, so every command
// that accepts a session_id inherits this; we cover read, stats, and expand
// (expand calls ResolveSession directly rather than via the helper).
func TestRunCommands_WhenSessionIDIsEmpty_ThenReturnsRequiredError(t *testing.T) {
	tests := []struct {
		name string
		run  func(args []string, out, errOut *bytes.Buffer, store parser.Store) error
		args []string
	}{
		{
			name: "read",
			run:  func(a []string, o, e *bytes.Buffer, s parser.Store) error { return runRead(a, o, e, s, testReader) },
			args: []string{""},
		},
		{
			name: "stats",
			run:  func(a []string, o, e *bytes.Buffer, s parser.Store) error { return runStats(a, o, e, s, testReader) },
			args: []string{""},
		},
		{
			name: "expand",
			run:  func(a []string, o, e *bytes.Buffer, s parser.Store) error { return runExpand(a, o, e, s, testReader) },
			args: []string{"", "uCVa"}, // expand needs a tool ID arg too
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := tt.run(tt.args, &stdout, &stderr, parser.Store{})
			if err == nil {
				t.Fatalf("%s with empty session_id returned nil error", tt.name)
			}
			if !strings.Contains(err.Error(), "required") {
				t.Fatalf("%s error = %v, want 'required'", tt.name, err)
			}
			if strings.Contains(err.Error(), "ambiguous") {
				t.Fatalf("%s error = %v, must not mention 'ambiguous'", tt.name, err)
			}
		})
	}
}

func TestRunRead_WhenSessionIDIsMissing_ThenReturnsError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runRead(nil, &stdout, &stderr, parser.Store{}, testReader)
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

	err := runRead([]string{sid}, &stdout, &stderr, store, testReader)
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

	err := runContext([]string{sid}, &stdout, &stderr, store, testReader)
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

	err := runStats([]string{"--no-tokens", sid}, &stdout, &stderr, store, testReader)
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

	// Empty key + no config.json => CountTokensAPI returns an error before
	// any network call, so both goroutines fail and runStats takes the estimate branch.
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("HOME", root)

	var stdout, stderr bytes.Buffer
	// No --no-tokens: we want the token-counting path to actually run.
	if err := runStats([]string{sid}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runStats returned error: %v", err)
	}
	got := stdout.String()

	// Proves we took the fallback branch, not the API branch nor --no-tokens.
	if !strings.Contains(got, "=== Tokens (estimated) ===") {
		t.Fatalf("stdout missing estimated-tokens header:\n%s", got)
	}
	// UX: the fallback must explain itself on stderr so the user knows why they
	// got an estimate. The diagnostic belongs on stderr, never in the stdout
	// payload — assert both. A mutation that drops the warning turns this red.
	if !strings.Contains(stderr.String(), "hint: to see token counts") {
		t.Fatalf("stderr missing config hint:\n%s", stderr.String())
	}
	if strings.Contains(got, "hint:") {
		t.Fatalf("hint leaked into stdout payload:\n%s", got)
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
	if !strings.Contains(got, "Raw:      "+pad10(analyzer.FormatNumber(rawEst))+" ~") {
		t.Fatalf("stdout missing raw estimate %s on Raw line:\n%s", analyzer.FormatNumber(rawEst), got)
	}
	if !strings.Contains(got, "Filtered: "+pad10(analyzer.FormatNumber(filtEst))+" ~") {
		t.Fatalf("stdout missing filtered estimate %s on Filtered line:\n%s", analyzer.FormatNumber(filtEst), got)
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
	if err := runStats([]string{sid}, &stdout, &stderr, store, testReader); err != nil {
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
	if !strings.Contains(got, "Raw:      "+pad10(analyzer.FormatNumber(rawCount))+"\n") {
		t.Fatalf("stdout missing raw API count %d on Raw line:\n%s", rawCount, got)
	}
	if !strings.Contains(got, "Filtered: "+pad10(analyzer.FormatNumber(filtCount))+"\n") {
		t.Fatalf("stdout missing filtered API count %d on Filtered line:\n%s", filtCount, got)
	}
	// Saved = raw - filtered must also be printed (guards the saved math).
	if !strings.Contains(got, "Saved:    "+pad10(analyzer.FormatNumber(rawCount-filtCount))) {
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

// Regression (F1): `audit -n -1` (and 0) used to be silently accepted, sampling
// zero items and exiting 0 — the user got an empty result with no feedback that
// their -n was nonsensical. The -n sample count must be a positive integer; any
// value < 1 is a validation error that fails the command before reading the
// transcript. Spec: -n "number of samples per category" requires >= 1.
func TestRunAudit_WhenSampleCountIsNotPositive_ThenReturnsValidationError(t *testing.T) {
	tests := []struct {
		name  string
		nFlag string
	}{
		{name: "negative", nFlag: "-1"},
		{name: "zero", nFlag: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			// Store is irrelevant: validation must reject before any session lookup.
			err := runAudit([]string{"anysession", "-n", tt.nFlag}, &stdout, &stderr, parser.Store{}, testReader)
			if err == nil {
				t.Fatalf("runAudit(-n %s) returned nil error, want validation error", tt.nFlag)
			}
			if !strings.Contains(err.Error(), "-n must be a positive integer") {
				t.Fatalf("error = %v, want '-n must be a positive integer'", err)
			}
		})
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
	err := runExpand([]string{sid, "uCVa"}, &stdout, &stderr, store, testReader)
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
	err := runExpand([]string{sid, "uCVa"}, &stdout, &stderr, store, testReader)

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
	if err := runExpand([]string{sid, secondID}, &stdout, &stderr, store, testReader); err != nil {
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
	err := runExpand([]string{sid, "ZZZZ"}, &stdout, &stderr, store, testReader)
	if err == nil {
		t.Fatal("runExpand should return error when no tool IDs match")
	}
	if !strings.Contains(err.Error(), "no matching tool IDs found") {
		t.Fatalf("error = %v, want 'no matching tool IDs found'", err)
	}
}

func TestRunExpand_GivenNoArgs_WhenCalled_ThenReturnsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runExpand(nil, &stdout, &stderr, parser.Store{}, testReader)
	if err == nil {
		t.Fatal("runExpand should return error with no args")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("error = %v, want usage message", err)
	}
}

// writeVerboseCLIFixture builds a session whose transcript carries the two
// payloads the verbose flags gate: an assistant thinking block and a slash
// command invocation (which surfaces as a "[/qa]" marker, plus its stdout as
// droppable command noise). Returns root and session id for runRead/runContext.
func writeVerboseCLIFixture(t *testing.T) (string, string) {
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
		`{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"<command-name>/qa</command-name>\n<command-message>qa</command-message>\n<command-args></command-args>"}}`,
		`{"type":"user","timestamp":"2026-05-28T00:00:01Z","message":{"role":"user","content":"<local-command-stdout>QA_STDOUT_PAYLOAD</local-command-stdout>"}}`,
		`{"type":"assistant","timestamp":"2026-05-28T00:00:02Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"SECRET_THINKING_PAYLOAD"},{"type":"text","text":"done"}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	writeListMeta(t, metaDir, sid, "/tmp/proj", "hello")
	return root, sid
}

// The verbose flags are wired flag-string -> *flag.Bool -> FormatOptions inside
// runRead/runContext. The formatter-level tests build FormatOptions directly and
// thus skip that wiring: if a flag name were mistyped or a FormatOptions field
// left unassigned, no formatter test would catch it. These cases drive the real
// flag parser through runRead/runContext and assert each gated payload is hidden
// by default and revealed only when its flag string is passed. A mutation that
// drops the VerboseThinking or VerboseCommands assignment in runRead/runContext
// turns the corresponding "with flag" case red.
func TestRunReadContext_VerboseFlagWiring_GatesPayloadBehindFlagString(t *testing.T) {
	const (
		thinkingPayload = "SECRET_THINKING_PAYLOAD"
		commandPayload  = "QA_STDOUT_PAYLOAD"
	)
	commands := []struct {
		name string
		run  func(args []string, out, errOut *bytes.Buffer, store parser.Store) error
	}{
		{
			name: "read",
			run:  func(a []string, o, e *bytes.Buffer, s parser.Store) error { return runRead(a, o, e, s, testReader) },
		},
		{
			name: "context",
			run:  func(a []string, o, e *bytes.Buffer, s parser.Store) error { return runContext(a, o, e, s, testReader) },
		},
	}
	cases := []struct {
		name    string
		flag    string
		payload string
	}{
		{name: "verbose-thinking reveals thinking", flag: "-verbose-thinking", payload: thinkingPayload},
		{name: "verbose-commands reveals command output", flag: "-verbose-commands", payload: commandPayload},
	}

	for _, cmd := range commands {
		for _, tc := range cases {
			t.Run(cmd.name+"/"+tc.name, func(t *testing.T) {
				root, sid := writeVerboseCLIFixture(t)
				store := parser.Store{
					ProjectsDir:    filepath.Join(root, ".claude", "projects"),
					SessionMetaDir: filepath.Join(root, ".claude", "usage-data", "session-meta"),
				}

				var noFlagOut, noFlagErr bytes.Buffer
				if err := cmd.run([]string{sid}, &noFlagOut, &noFlagErr, store); err != nil {
					t.Fatalf("%s without flag returned error: %v", cmd.name, err)
				}
				if strings.Contains(noFlagOut.String(), tc.payload) {
					t.Fatalf("%s leaked %q without %s:\n%s", cmd.name, tc.payload, tc.flag, noFlagOut.String())
				}

				var withFlagOut, withFlagErr bytes.Buffer
				if err := cmd.run([]string{sid, tc.flag}, &withFlagOut, &withFlagErr, store); err != nil {
					t.Fatalf("%s with %s returned error: %v", cmd.name, tc.flag, err)
				}
				if !strings.Contains(withFlagOut.String(), tc.payload) {
					t.Fatalf("%s did not reveal %q with %s:\n%s", cmd.name, tc.payload, tc.flag, withFlagOut.String())
				}
			})
		}
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

func TestRunList_GivenJSONLWithoutMetadata_ThenShowsFallbackSession(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "projects", "-Users-me-proj")
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}
	sid := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	transcript := strings.Join([]string{
		`{"type":"user","timestamp":"2026-06-15T10:00:00+00:00","message":{"role":"user","content":"help me understand channels"}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	// Deliberately no metadata file for this session

	var stdout, stderr bytes.Buffer
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, "projects"),
		SessionMetaDir: metaDir,
		HeaderScanner:  claudecodec.Codec{},
	}
	err := runList(nil, &stdout, &stderr, store)
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, sid) {
		t.Fatalf("stdout missing session ID %s (JSONL fallback session not listed):\n%s", sid, got)
	}
	if !strings.Contains(got, "help me understand channels") {
		t.Fatalf("stdout missing first prompt from JSONL fallback:\n%s", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty for JSONL-only session", stderr.String())
	}
}

func TestRunList_GivenJSONLAndMeta_ThenMetaWinsAndNoDuplicates(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "projects", "-Users-me-proj")
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}
	sid := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

	// JSONL with a different prompt than the metadata
	transcript := strings.Join([]string{
		`{"type":"user","timestamp":"2026-06-15T10:00:00+00:00","message":{"role":"user","content":"jsonl prompt here"}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	// Metadata for the same session with canonical values
	writeListMeta(t, metaDir, sid, "/Users/me/proj", "meta prompt here")

	var stdout, stderr bytes.Buffer
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, "projects"),
		SessionMetaDir: metaDir,
		HeaderScanner:  claudecodec.Codec{},
	}
	err := runList(nil, &stdout, &stderr, store)
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	got := stdout.String()
	// Should appear exactly once (no duplicate from JSONL fallback)
	if count := strings.Count(got, sid); count != 1 {
		t.Fatalf("session ID %s appears %d times in output, want exactly 1:\n%s", sid, count, got)
	}
	// Metadata prompt wins over JSONL prompt
	if !strings.Contains(got, "meta prompt here") {
		t.Fatalf("stdout missing metadata first_prompt (meta must win over JSONL):\n%s", got)
	}
	if strings.Contains(got, "jsonl prompt here") {
		t.Fatalf("stdout contains JSONL prompt (meta should win):\n%s", got)
	}
}

func TestRunList_GivenJSONLOnly_WhenProjectFilterApplied_ThenFiltersCorrectly(t *testing.T) {
	root := t.TempDir()
	projectDirMatch := filepath.Join(root, "projects", "-Users-me-myapi")
	projectDirOther := filepath.Join(root, "projects", "-Users-me-webapp")
	metaDir := filepath.Join(root, "session-meta")
	for _, d := range []string{projectDirMatch, projectDirOther, metaDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("create dir %s: %v", d, err)
		}
	}

	sidMatch := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	sidOther := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	for _, tc := range []struct {
		sid    string
		dir    string
		prompt string
	}{
		{sid: sidMatch, dir: projectDirMatch, prompt: "api question"},
		{sid: sidOther, dir: projectDirOther, prompt: "web question"},
	} {
		transcript := `{"type":"user","timestamp":"2026-06-15T10:00:00+00:00","message":{"role":"user","content":"` + tc.prompt + `"}}` + "\n"
		if err := os.WriteFile(filepath.Join(tc.dir, tc.sid+".jsonl"), []byte(transcript), 0o644); err != nil {
			t.Fatalf("write transcript: %v", err)
		}
	}

	var stdout, stderr bytes.Buffer
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, "projects"),
		SessionMetaDir: metaDir,
		HeaderScanner:  claudecodec.Codec{},
	}
	err := runList([]string{"-p", "myapi"}, &stdout, &stderr, store)
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "api question") {
		t.Fatalf("stdout missing matched JSONL session:\n%s", got)
	}
	if strings.Contains(got, "web question") {
		t.Fatalf("stdout includes filtered-out session:\n%s", got)
	}
}

// writeLargeCLIFixture creates a session transcript with many user messages
// so that the default 200-line cap is triggered. Each user message renders as
// 3 output lines (header, body, blank), so 100 messages = 300 lines > 200.
func writeLargeCLIFixture(t *testing.T) (string, string) {
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

	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, `{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"msg`+strings.Repeat("x", 5)+` `+fmt.Sprintf("%d", i)+`"}}`)
	}
	lines = append(lines, "")
	transcript := strings.Join(lines, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	writeListMeta(t, metaDir, sid, "/tmp/proj", "hello")
	return root, sid
}

// TestRunRead_GivenLargeSession_WhenDefaultFlags_ThenTruncatesAt200
// verifies that runRead with default flags (max-lines=200) clips output at 200
// lines and adds a truncation message naming the resume offset.
func TestRunRead_GivenLargeSession_WhenDefaultFlags_ThenTruncatesAt200(t *testing.T) {
	root, sid := writeLargeCLIFixture(t)
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, ".claude", "projects"),
		SessionMetaDir: filepath.Join(root, ".claude", "usage-data", "session-meta"),
	}
	var stdout, stderr bytes.Buffer
	if err := runRead([]string{sid}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runRead returned error: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "--- truncated at line 200") {
		t.Fatalf("default runRead must truncate at 200 lines:\n%s", got)
	}
	if !strings.Contains(got, "use --offset 200 to continue") {
		t.Fatalf("truncation message must include --offset 200 hint:\n%s", got)
	}
}

// TestRunRead_GivenLargeSession_WhenOffsetFlag_ThenSkipsLines
// verifies that -offset N shifts the output window so lines before N are absent.
func TestRunRead_GivenLargeSession_WhenOffsetFlag_ThenSkipsLines(t *testing.T) {
	root, sid := writeLargeCLIFixture(t)
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, ".claude", "projects"),
		SessionMetaDir: filepath.Join(root, ".claude", "usage-data", "session-meta"),
	}
	var stdout, stderr bytes.Buffer
	// -max-lines 0 (unlimited) so we see whether offset works independently.
	if err := runRead([]string{sid, "-offset", "3", "-max-lines", "0"}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runRead with -offset 3 returned error: %v", err)
	}
	got := stdout.String()
	// With offset=3 the first message block (lines 0-2: header, body, blank) is
	// skipped. "msgxxxxx 0" (message body on line 1) must be absent.
	if strings.Contains(got, "msgxxxxx 0") {
		t.Fatalf("offset=3 must skip message 0 body; still present:\n%s", got)
	}
	// Later messages must still appear.
	if !strings.Contains(got, "msgxxxxx 5") {
		t.Fatalf("offset=3 output must include message 5:\n%s", got)
	}
}

// TestRunRead_GivenLargeSession_WhenMaxLinesZero_ThenEmitsUnlimitedOutput
// verifies that -max-lines 0 disables the 200-line default cap.
func TestRunRead_GivenLargeSession_WhenMaxLinesZero_ThenEmitsUnlimitedOutput(t *testing.T) {
	root, sid := writeLargeCLIFixture(t)
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, ".claude", "projects"),
		SessionMetaDir: filepath.Join(root, ".claude", "usage-data", "session-meta"),
	}
	var stdout, stderr bytes.Buffer
	if err := runRead([]string{sid, "-max-lines", "0"}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runRead(-max-lines 0) returned error: %v", err)
	}
	got := stdout.String()
	if strings.Contains(got, "truncated") {
		t.Fatalf("-max-lines 0 must disable truncation:\n%s", got)
	}
	// All 100 messages must appear.
	if !strings.Contains(got, "msgxxxxx 99") {
		t.Fatalf("-max-lines 0 must emit all messages including 99:\n%s", got)
	}
}

// TestRunContext_GivenLargeSession_WhenDefaultFlags_ThenTruncatesAt200
// verifies that runContext with default flags clips at 200 lines.
func TestRunContext_GivenLargeSession_WhenDefaultFlags_ThenTruncatesAt200(t *testing.T) {
	root, sid := writeLargeCLIFixture(t)
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, ".claude", "projects"),
		SessionMetaDir: filepath.Join(root, ".claude", "usage-data", "session-meta"),
	}
	var stdout, stderr bytes.Buffer
	if err := runContext([]string{sid}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runContext returned error: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "--- truncated at line 200") {
		t.Fatalf("default runContext must truncate at 200 lines:\n%s", got)
	}
	if !strings.Contains(got, "use --offset 200 to continue") {
		t.Fatalf("truncation message must include --offset 200 hint:\n%s", got)
	}
}

// TestRunContext_GivenLargeSession_WhenOffsetAndMaxLines_ThenWindowsOutput
// verifies that -offset and -max-lines combine correctly in context mode.
func TestRunContext_GivenLargeSession_WhenOffsetAndMaxLines_ThenWindowsOutput(t *testing.T) {
	root, sid := writeLargeCLIFixture(t)
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, ".claude", "projects"),
		SessionMetaDir: filepath.Join(root, ".claude", "usage-data", "session-meta"),
	}
	var stdout, stderr bytes.Buffer
	if err := runContext([]string{sid, "-offset", "3", "-max-lines", "3"}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runContext with -offset and -max-lines returned error: %v", err)
	}
	got := stdout.String()
	// offset=3, maxLines=3 → lines 3,4,5 of the context output.
	// The context header ("# Session ...\n\n") occupies 2 lines, so
	// the context body starts at line 2 (0-indexed). Line 3 is the first message
	// body. Whatever appears, the truncation message must cite offset=6.
	if !strings.Contains(got, "--- truncated at line 6") {
		t.Fatalf("offset=3+maxLines=3 context must truncate at line 6:\n%s", got)
	}
}

// TestRunRead_GivenNegativeOffset_WhenCalled_ThenReturnsValidationError
// verifies that a negative offset is rejected before any session lookup.
func TestRunRead_GivenNegativeOffset_WhenCalled_ThenReturnsValidationError(t *testing.T) {
	root, sid := writeCLIFixture(t)
	store := parser.Store{
		ProjectsDir:    filepath.Join(root, ".claude", "projects"),
		SessionMetaDir: filepath.Join(root, ".claude", "usage-data", "session-meta"),
	}
	var stdout, stderr bytes.Buffer
	err := runRead([]string{sid, "-offset", "-1"}, &stdout, &stderr, store, testReader)
	if err == nil {
		t.Fatal("runRead(-offset -1) returned nil error, want validation error")
	}
	if !strings.Contains(err.Error(), "-offset") {
		t.Fatalf("error = %v, want -offset validation message", err)
	}
}

// --- Help flag handling ---

func TestRunRead_GivenHelpFlag_ThenReturnsErrHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runRead([]string{"-h"}, &stdout, &stderr, parser.Store{}, testReader)
	if err == nil || err.Error() != "flag: help requested" {
		t.Fatalf("runRead(-h) = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(stderr.String(), "max-lines") {
		t.Fatalf("stderr should contain flag descriptions, got: %q", stderr.String())
	}
}

func TestRunStats_GivenHelpFlag_ThenReturnsErrHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runStats([]string{"-h"}, &stdout, &stderr, parser.Store{}, testReader)
	if err == nil || err.Error() != "flag: help requested" {
		t.Fatalf("runStats(-h) = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(stderr.String(), "no-tokens") {
		t.Fatalf("stderr should contain flag descriptions, got: %q", stderr.String())
	}
}

func TestRunList_GivenHelpFlag_ThenReturnsErrHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runList([]string{"-h"}, &stdout, &stderr, parser.Store{})
	if err == nil || err.Error() != "flag: help requested" {
		t.Fatalf("runList(-h) = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(stderr.String(), "-n") {
		t.Fatalf("stderr should contain flag descriptions, got: %q", stderr.String())
	}
}

func TestRunInject_GivenHelpFlag_ThenReturnsErrHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runInject([]string{"-h"}, &stdout, &stderr, parser.Store{}, testReader)
	if err == nil || err.Error() != "flag: help requested" {
		t.Fatalf("runInject(-h) = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(stderr.String(), "page") {
		t.Fatalf("stderr should contain flag descriptions, got: %q", stderr.String())
	}
}

func TestRunContext_GivenHelpFlag_ThenReturnsErrHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runContext([]string{"-h"}, &stdout, &stderr, parser.Store{}, testReader)
	if err == nil || err.Error() != "flag: help requested" {
		t.Fatalf("runContext(-h) = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(stderr.String(), "max-lines") {
		t.Fatalf("stderr should contain flag descriptions, got: %q", stderr.String())
	}
}

func TestRunAudit_GivenHelpFlag_ThenReturnsErrHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runAudit([]string{"-h"}, &stdout, &stderr, parser.Store{}, testReader)
	if err == nil || err.Error() != "flag: help requested" {
		t.Fatalf("runAudit(-h) = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(stderr.String(), "-n") {
		t.Fatalf("stderr should contain flag descriptions, got: %q", stderr.String())
	}
}

func TestRunExpand_GivenHelpFlag_ThenReturnsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runExpand([]string{"-h"}, &stdout, &stderr, parser.Store{}, testReader)
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("runExpand(-h) = %v, want usage error", err)
	}
}

func TestRunUsage_GivenHelpFlag_ThenReturnsErrHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runUsage([]string{"-h"}, &stdout, &stderr)
	if err == nil || err.Error() != "flag: help requested" {
		t.Fatalf("runUsage(-h) = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(stderr.String(), "-cmd") {
		t.Fatalf("stderr should contain flag descriptions, got: %q", stderr.String())
	}
}

// Guards against regression where exitOnError would print "Error: <nil>" or
// "Error: flag: help requested" to stderr instead of being silent.
func TestExitOnError_GivenNil_ThenNoStderrOutput(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	exitOnError(nil)
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	if buf.Len() > 0 {
		t.Fatalf("exitOnError(nil) wrote to stderr: %q", buf.String())
	}
}

// Guards against regression where exitOnError would print the ErrHelp message.
func TestExitOnError_GivenErrHelp_ThenNoStderrOutput(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	exitOnError(flag.ErrHelp)
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	if buf.Len() > 0 {
		t.Fatalf("exitOnError(flag.ErrHelp) wrote to stderr: %q", buf.String())
	}
}
