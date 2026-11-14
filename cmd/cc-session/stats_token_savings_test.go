package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/analyzer"
	bm "github.com/Mapleeeeeeeeeee/cc-session-reader/internal/benchmark"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
)

const (
	savingsTestSessionID = "55555555-5555-5555-5555-555555555555"
	// The usage in writeBenchmarkSession puts the session's last context at 100,000 tokens.
	savingsTestContextTokens  = 100_000
	savingsTestFilteredTokens = 20_000
)

func writeTokenSavingsSession(t *testing.T) parser.Store {
	t.Helper()
	root := t.TempDir()
	writeBenchmarkSession(t, root, savingsTestSessionID,
		`{"input_tokens":99000,"cache_creation_input_tokens":1000,"cache_creation":{"ephemeral_5m_input_tokens":1000,"ephemeral_1h_input_tokens":0},"cache_read_input_tokens":0,"output_tokens":1000}`)
	store := parser.Store{ProjectsDir: filepath.Join(root, "projects"), SessionMetaDir: filepath.Join(root, "session-meta")}

	original := newCountTokensFn
	t.Cleanup(func() { newCountTokensFn = original })
	newCountTokensFn = func(model string) (countTokensFunc, error) {
		return func(text string) (int, error) { return savingsTestFilteredTokens, nil }, nil
	}
	return store
}

func runStatsTokenSavings(t *testing.T, store parser.Store, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if err := runStats(append(args, savingsTestSessionID), &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runStats returned error: %v", err)
	}
	section := outputSection(stdout.String(), "=== Token Savings ===")
	if section == "" {
		t.Fatalf("stats output has no Token Savings section:\n%s", stdout.String())
	}
	return section
}

// Regression: stats measured Saved against the filtered text alone, leaving the new session's overhead out and overstating the saving (v0.1.84)
func TestRunStats_GivenOverhead_WhenRenderingTokenSavings_ThenNewContextIncludesOverhead(t *testing.T) {
	cases := []struct {
		name         string
		args         []string
		overhead     int
		wantSavedPct string
	}{
		{name: "overhead flag", args: []string{"-overhead", "30000"}, overhead: 30_000, wantSavedPct: "50.0%"},
		{name: "default overhead", args: nil, overhead: bm.DefaultOverhead, wantSavedPct: "40.0%"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := writeTokenSavingsSession(t)

			section := runStatsTokenSavings(t, store, tc.args...)

			wantNewContext := analyzer.FormatNumber(tc.overhead + savingsTestFilteredTokens)
			if row := outputLineContaining(section, "New context:"); !strings.Contains(row, wantNewContext) {
				t.Errorf("New context row should show overhead + filtered = %s:\n%s", wantNewContext, section)
			}
			wantSaved := analyzer.FormatNumber(savingsTestContextTokens - tc.overhead - savingsTestFilteredTokens)
			row := outputLineContaining(section, "Saved:")
			if !strings.Contains(row, wantSaved) || !strings.Contains(row, tc.wantSavedPct) {
				t.Errorf("Saved row should show %s (%s):\n%s", wantSaved, tc.wantSavedPct, section)
			}
		})
	}
}

func TestRunStats_GivenSameSessionAndOverhead_ThenSavedPercentMatchesBenchmark(t *testing.T) {
	store := writeTokenSavingsSession(t)

	statsSection := runStatsTokenSavings(t, store, "-overhead", "30000")

	var benchmarkOut, benchmarkErr bytes.Buffer
	args := []string{"--n", "1", "--min-kb", "0", "--overhead", "30000"}
	if err := runBenchmark(args, &benchmarkOut, &benchmarkErr, store, testReader); err != nil {
		t.Fatalf("runBenchmark returned error: %v", err)
	}
	compressionRow := outputLineContaining(benchmarkOut.String(), "55555555")
	fields := strings.Fields(compressionRow)
	if len(fields) < 5 {
		t.Fatalf("unexpected benchmark row %q in output:\n%s", compressionRow, benchmarkOut.String())
	}
	benchmarkSavedPct := fields[4]

	if row := outputLineContaining(statsSection, "Saved:"); !strings.Contains(row, fmt.Sprintf("(%s)", benchmarkSavedPct)) {
		t.Errorf("stats Saved should match benchmark Saved %s:\nstats:\n%s\nbenchmark row: %s", benchmarkSavedPct, statsSection, compressionRow)
	}
}
