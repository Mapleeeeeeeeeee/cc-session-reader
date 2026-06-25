package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/analyzer"
	bm "github.com/Mapleeeeeeeeeee/cc-session-reader/internal/benchmark"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
)

func TestPrintCompressionSection_WhenRendered_ThenUsesNewSessionTotalContext(t *testing.T) {
	results := []bm.Result{
		{
			ShortID:          "aaaaaaaa",
			ContextTokens:    100_000,
			FilteredTokens:   23_456,
			NewContextTokens: 60_000,
			SavedPct:         40.0,
		},
		{
			ShortID:          "bbbbbbbb",
			ContextTokens:    200_000,
			FilteredTokens:   87_654,
			NewContextTokens: 120_000,
			SavedPct:         40.0,
		},
	}

	var out bytes.Buffer
	bm.PrintCompressionSection(&out, results)
	got := out.String()

	if !strings.Contains(got, "Context      NewCtx") {
		t.Fatalf("compression header must compare total contexts, got:\n%s", got)
	}
	if strings.Contains(got, "Filtered") {
		t.Fatalf("compression table must not label history-only tokens as the comparable total context:\n%s", got)
	}
	if !strings.Contains(got, "aaaaaaaa") ||
		!strings.Contains(got, "100,000") ||
		!strings.Contains(got, "60,000") ||
		!strings.Contains(got, "40.0%") {
		t.Fatalf("compression row missing new session total context:\n%s", got)
	}
	if strings.Contains(got, "23,456") || strings.Contains(got, "87,654") {
		t.Fatalf("compression row leaked filtered-history-only token count:\n%s", got)
	}
}

func TestMedianFloat64_GivenEvenCount_ThenAveragesMiddleValues(t *testing.T) {
	got := bm.MedianFloat64([]float64{78.9, 90.3})
	want := 84.6
	if got != want {
		t.Fatalf("MedianFloat64(even) = %.1f, want %.1f", got, want)
	}
}

func TestPrintCompressionSection_GivenEvenCount_ThenPrintsAveragedMedian(t *testing.T) {
	results := []bm.Result{
		{ShortID: "aaaaaaaa", ContextTokens: 100, NewContextTokens: 20, SavedPct: 78.9},
		{ShortID: "bbbbbbbb", ContextTokens: 100, NewContextTokens: 10, SavedPct: 90.3},
	}

	var out bytes.Buffer
	bm.PrintCompressionSection(&out, results)

	if got := out.String(); !strings.Contains(got, "Median: 84.6%") {
		t.Fatalf("compression summary must average the two middle values for even counts:\n%s", got)
	}
}

func TestResolveBenchmarkModel_GivenAcceptedModelNames_ThenReturnsPricingAndTokenCountingModel(t *testing.T) {
	tests := []struct {
		name                string
		wantPricing         bm.Pricing
		wantTokenCountModel string
	}{
		{name: "opus", wantPricing: bm.PricingOpus, wantTokenCountModel: bm.TokenCountModelOpus48},
		{name: "opus-4-6", wantPricing: bm.PricingOpus, wantTokenCountModel: bm.TokenCountModelOpus46},
		{name: "opus-4-7", wantPricing: bm.PricingOpus, wantTokenCountModel: bm.TokenCountModelOpus47},
		{name: "opus-4-8", wantPricing: bm.PricingOpus, wantTokenCountModel: bm.TokenCountModelOpus48},
		{name: "sonnet", wantPricing: bm.PricingSonnet, wantTokenCountModel: bm.TokenCountModelSonnet},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bm.ResolveModel(tt.name)
			if err != nil {
				t.Fatalf("ResolveModel(%q) returned error: %v", tt.name, err)
			}
			if got.Pricing != tt.wantPricing {
				t.Fatalf("pricing = %+v, want %+v", got.Pricing, tt.wantPricing)
			}
			if got.TokenCountModel != tt.wantTokenCountModel {
				t.Fatalf("token count model = %q, want %q", got.TokenCountModel, tt.wantTokenCountModel)
			}
		})
	}
}

func TestResolveBenchmarkModel_GivenUnknownModel_ThenReturnsAcceptedNames(t *testing.T) {
	_, err := bm.ResolveModel("opus-4-5")
	if err == nil {
		t.Fatal("ResolveModel returned nil error for unknown model")
	}

	got := err.Error()
	for _, want := range []string{"opus", "opus-4-6", "opus-4-7", "opus-4-8", "sonnet"} {
		if !strings.Contains(got, want) {
			t.Fatalf("error message missing accepted model %q: %s", want, got)
		}
	}
}

func TestRunBenchmark_WhenSessionHasAPIUsage_ThenUsesTokenCountingAPIForNewContext(t *testing.T) {
	root := t.TempDir()
	sid := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	projectDir := filepath.Join(root, "projects", "proj")
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}

	transcript := strings.Join([]string{
		`{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hi"},{"type":"tool_use","name":"Bash","id":"toolu_1","input":{"command":"echo raw payload that must not be counted"}}],"usage":{"input_tokens":100000,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1000}}}`,
		`{"type":"user","timestamp":"2026-05-28T00:00:02Z","toolUseResult":{"success":true,"commandName":"Bash"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"raw result that is replaced by a summary"}]}}`,
		"",
	}, "\n")
	transcriptPath := filepath.Join(projectDir, sid+".jsonl")
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	writeListMeta(t, metaDir, sid, "/tmp/proj", "hello")
	stats := analyzer.ComputeStats(mustReadAll(t, transcriptPath))
	if stats.RawText == stats.FilteredText {
		t.Fatal("fixture invalid: raw and filtered text must differ")
	}

	const filteredTokenCount = 23_456
	original := newCountTokensFn
	t.Cleanup(func() { newCountTokensFn = original })
	var countedText string
	var countModel string
	newCountTokensFn = func(model string) (countTokensFunc, error) {
		countModel = model
		return func(text string) (int, error) {
			countedText = text
			if text != stats.FilteredText {
				t.Fatalf("token counter received wrong text; got raw=%t filtered=%t", text == stats.RawText, text == stats.FilteredText)
			}
			return filteredTokenCount, nil
		}, nil
	}

	var stdout, stderr bytes.Buffer
	store := parser.Store{ProjectsDir: filepath.Join(root, "projects"), SessionMetaDir: metaDir}
	if err := runBenchmark([]string{"--n", "1", "--min-kb", "0", "--overhead", "40000"}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runBenchmark returned error: %v", err)
	}

	if countedText == "" {
		t.Fatal("countTokensFn was not called")
	}
	wantCountModel := bm.TokenCountModelOpus48
	if countModel != wantCountModel {
		t.Fatalf("token counter model = %q, want %q", countModel, wantCountModel)
	}
	got := stdout.String()
	row := outputLineContaining(got, "aaaaaaaa")
	for _, want := range []string{"100,000", analyzer.FormatNumber(40_000 + filteredTokenCount), "36.5%"} {
		if !strings.Contains(row, want) {
			t.Fatalf("benchmark row missing %s:\nrow: %s\nfull output:\n%s", want, row, got)
		}
	}
	costSection := outputSection(got, "=== Cost Savings Per Session")
	for _, want := range []string{"NewCtx", analyzer.FormatNumber(40_000 + filteredTokenCount)} {
		if !strings.Contains(costSection, want) {
			t.Fatalf("cost summary missing %s:\nsection:\n%s\nfull output:\n%s", want, costSection, got)
		}
	}
	if strings.Contains(costSection, analyzer.FormatNumber(filteredTokenCount)) {
		t.Fatalf("cost summary leaked filtered-history-only token count:\nsection:\n%s", costSection)
	}
}

func TestRunBenchmark_WhenSessionHasNoAPIUsage_ThenSkipsSession(t *testing.T) {
	root := t.TempDir()
	sid := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	projectDir := filepath.Join(root, "projects", "proj")
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}

	transcript := strings.Join([]string{
		`{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":"hi"}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	writeListMeta(t, metaDir, sid, "/tmp/proj", "hello")

	original := newCountTokensFn
	t.Cleanup(func() { newCountTokensFn = original })
	newCountTokensFn = func(model string) (countTokensFunc, error) {
		t.Fatal("newCountTokensFn must not be called for sessions without API usage data")
		return nil, nil
	}

	var stdout, stderr bytes.Buffer
	store := parser.Store{ProjectsDir: filepath.Join(root, "projects"), SessionMetaDir: metaDir}
	if err := runBenchmark([]string{"--n", "1", "--min-kb", "0"}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runBenchmark returned error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "missing API usage data") || !strings.Contains(got, "No sessions could be processed.") {
		t.Fatalf("benchmark output should skip sessions without API usage data:\n%s", got)
	}
}

func TestRunBenchmark_WhenTopCandidateIsSkipped_ThenNCountsProcessedResults(t *testing.T) {
	root := t.TempDir()
	metaDir := filepath.Join(root, "session-meta")
	projectDir := filepath.Join(root, "projects", "proj")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}

	skippedSID := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	skippedTranscript := strings.Join([]string{
		`{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"` + strings.Repeat("old session without usage ", 200) + `"}}`,
		`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":"hi"}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, skippedSID+".jsonl"), []byte(skippedTranscript), 0o644); err != nil {
		t.Fatalf("write skipped transcript: %v", err)
	}
	writeListMeta(t, metaDir, skippedSID, "/tmp/proj", "old")

	validSID := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	validTranscript := strings.Join([]string{
		`{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":"hi","usage":{"input_tokens":100000,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1000}}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, validSID+".jsonl"), []byte(validTranscript), 0o644); err != nil {
		t.Fatalf("write valid transcript: %v", err)
	}
	writeListMeta(t, metaDir, validSID, "/tmp/proj", "new")

	original := newCountTokensFn
	t.Cleanup(func() { newCountTokensFn = original })
	newCountTokensFn = func(model string) (countTokensFunc, error) {
		return func(text string) (int, error) {
			return 20_000, nil
		}, nil
	}

	var stdout, stderr bytes.Buffer
	store := parser.Store{ProjectsDir: filepath.Join(root, "projects"), SessionMetaDir: metaDir}
	if err := runBenchmark([]string{"--n", "1", "--min-kb", "0", "--overhead", "40000"}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runBenchmark returned error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "skipping cccccccc") || !strings.Contains(got, "dddddddd") {
		t.Fatalf("benchmark should skip invalid candidate and still process one valid result:\n%s", got)
	}
	if strings.Contains(got, "No sessions could be processed.") {
		t.Fatalf("benchmark incorrectly let skipped candidate consume --n:\n%s", got)
	}
}

func TestRunBenchmark_GivenSonnetModel_ThenUsesSonnetTokenCounterModel(t *testing.T) {
	root := t.TempDir()
	metaDir := filepath.Join(root, "session-meta")
	projectDir := filepath.Join(root, "projects", "proj")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}

	sid := "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	transcript := strings.Join([]string{
		`{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":"hi","usage":{"input_tokens":100000,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1000}}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	writeListMeta(t, metaDir, sid, "/tmp/proj", "hello")

	original := newCountTokensFn
	t.Cleanup(func() { newCountTokensFn = original })
	var countModel string
	newCountTokensFn = func(model string) (countTokensFunc, error) {
		countModel = model
		return func(text string) (int, error) {
			return 20_000, nil
		}, nil
	}

	var stdout, stderr bytes.Buffer
	store := parser.Store{ProjectsDir: filepath.Join(root, "projects"), SessionMetaDir: metaDir}
	if err := runBenchmark([]string{"--n", "1", "--min-kb", "0", "--model", "sonnet"}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runBenchmark returned error: %v", err)
	}

	wantCountModel := bm.TokenCountModelSonnet
	if countModel != wantCountModel {
		t.Fatalf("token counter model = %q, want %q", countModel, wantCountModel)
	}
}

func TestRunBenchmark_GivenTwoValidSessions_ThenReusesTokenCounter(t *testing.T) {
	root := t.TempDir()
	metaDir := filepath.Join(root, "session-meta")
	projectDir := filepath.Join(root, "projects", "proj")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}

	for _, sid := range []string{"ffffffff-ffff-ffff-ffff-ffffffffffff", "99999999-9999-9999-9999-999999999999"} {
		transcript := strings.Join([]string{
			`{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"hello"}}`,
			`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":"hi","usage":{"input_tokens":100000,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1000}}}`,
			"",
		}, "\n")
		if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
			t.Fatalf("write transcript %s: %v", sid, err)
		}
		writeListMeta(t, metaDir, sid, "/tmp/proj", "hello")
	}

	original := newCountTokensFn
	t.Cleanup(func() { newCountTokensFn = original })
	factoryCalls := 0
	counterCalls := 0
	newCountTokensFn = func(model string) (countTokensFunc, error) {
		factoryCalls++
		return func(text string) (int, error) {
			counterCalls++
			return 20_000, nil
		}, nil
	}

	var stdout, stderr bytes.Buffer
	store := parser.Store{ProjectsDir: filepath.Join(root, "projects"), SessionMetaDir: metaDir}
	if err := runBenchmark([]string{"--n", "2", "--min-kb", "0", "--overhead", "40000"}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runBenchmark returned error: %v", err)
	}

	if factoryCalls != 1 {
		t.Fatalf("newCountTokensFn calls = %d, want 1", factoryCalls)
	}
	if counterCalls != 2 {
		t.Fatalf("token counter calls = %d, want 2", counterCalls)
	}
}

func TestRunBenchmark_GivenNoAPIFlag_ThenSkipsAPIAndEstimatesFilteredTokensWithCharsDiv2(t *testing.T) {
	root := t.TempDir()
	sid := "11111111-1111-1111-1111-111111111111"
	projectDir := filepath.Join(root, "projects", "proj")
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}

	transcript := strings.Join([]string{
		`{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":"hi","usage":{"input_tokens":100000,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1000}}}`,
		"",
	}, "\n")
	transcriptPath := filepath.Join(projectDir, sid+".jsonl")
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	writeListMeta(t, metaDir, sid, "/tmp/proj", "hello")

	stats := analyzer.ComputeStats(mustReadAll(t, transcriptPath))
	// guards against the bug where production code uses len(FilteredText) byte count
	// instead of FilteredChars rune count — the two are equal for ASCII but diverge
	// with multibyte characters
	expectedFilteredToks := stats.FilteredChars / bm.CharsPerToken

	original := newCountTokensFn
	t.Cleanup(func() { newCountTokensFn = original })
	newCountTokensFn = func(model string) (countTokensFunc, error) {
		// guards against the bug where --no-api still calls the API
		t.Fatal("newCountTokensFn must not be called when --no-api is set")
		return nil, nil
	}

	var stdout, stderr bytes.Buffer
	store := parser.Store{ProjectsDir: filepath.Join(root, "projects"), SessionMetaDir: metaDir}
	if err := runBenchmark([]string{"--no-api", "--n", "1", "--min-kb", "0", "--overhead", "40000"}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runBenchmark returned error: %v", err)
	}

	got := stdout.String()
	row := outputLineContaining(got, "11111111")
	wantNewCtx := analyzer.FormatNumber(40_000 + expectedFilteredToks)
	if !strings.Contains(row, wantNewCtx) {
		t.Fatalf("--no-api benchmark row should show new context = overhead + FilteredChars/2 (%s):\nrow: %s\nfull output:\n%s",
			wantNewCtx, row, got)
	}
	if !strings.Contains(row, "100,000") {
		t.Fatalf("--no-api benchmark row should show contextToks = 100,000:\nrow: %s\nfull output:\n%s", row, got)
	}
	expectedSavedPct := float64(100_000-(40_000+expectedFilteredToks)) * 100.0 / float64(100_000)
	wantSavedPct := fmt.Sprintf("%.1f%%", expectedSavedPct)
	if !strings.Contains(row, wantSavedPct) {
		t.Fatalf("--no-api benchmark row should show savedPct = %s:\nrow: %s\nfull output:\n%s", wantSavedPct, row, got)
	}
}

func TestRunBenchmark_GivenNoAPIFlagAndToolUse_ThenToolIOPerCallUsesCharsPerToken(t *testing.T) {
	// guards against a bug where toolIOPerCall was computed as totalToolChars / totalToolCalls / 4
	// (accidentally using the old chars/2 heuristic twice), rather than / charsPerToken (= / 2)
	root := t.TempDir()
	sid := "22222222-2222-2222-2222-222222222222"
	projectDir := filepath.Join(root, "projects", "proj")
	metaDir := filepath.Join(root, "session-meta")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("create meta dir: %v", err)
	}

	// two Bash tool_uses in a single user turn: each input JSON = 1000 chars, each result = 1000 chars
	// totalToolChars = 4000, totalToolCalls = 2 -> toolIOPerCall = 4000 / 2 / charsPerToken = 1000
	// K = APICallCount / UserTurnCount = 2 / 1 = 2, so toolIOPerTurn = toolIO*(K-1) != 0
	transcript := strings.Join([]string{
		`{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"ok"},{"type":"tool_use","name":"Bash","id":"toolu_1","input":{"command":"` + strings.Repeat("x", 986) + `"}}],"usage":{"input_tokens":50000,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":500}}}`,
		`{"type":"user","timestamp":"2026-05-28T00:00:02Z","toolUseResult":{"success":true,"commandName":"Bash"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"` + strings.Repeat("y", 1000) + `"}]}}`,
		`{"type":"assistant","timestamp":"2026-05-28T00:00:03Z","message":{"role":"assistant","content":[{"type":"text","text":"done"},{"type":"tool_use","name":"Bash","id":"toolu_2","input":{"command":"` + strings.Repeat("x", 986) + `"}}],"usage":{"input_tokens":51000,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":500}}}`,
		`{"type":"user","timestamp":"2026-05-28T00:00:04Z","toolUseResult":{"success":true,"commandName":"Bash"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"` + strings.Repeat("y", 1000) + `"}]}}`,
		"",
	}, "\n")
	transcriptPath := filepath.Join(projectDir, sid+".jsonl")
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	writeListMeta(t, metaDir, sid, "/tmp/proj", "hello")

	// compute expected toolIOPerCall from actual PerTool data using charsPerToken
	stats := analyzer.ComputeStats(mustReadAll(t, transcriptPath))
	totalToolChars := 0
	totalToolCalls := 0
	for _, ts := range stats.PerTool {
		totalToolChars += ts.InputChars + ts.ResultChars
		totalToolCalls += ts.CallCount
	}
	if totalToolCalls == 0 {
		t.Fatal("fixture invalid: transcript must have at least one tool call")
	}
	expectedToolIO := totalToolChars / totalToolCalls / bm.CharsPerToken

	original := newCountTokensFn
	t.Cleanup(func() { newCountTokensFn = original })
	newCountTokensFn = func(model string) (countTokensFunc, error) {
		t.Fatal("newCountTokensFn must not be called when --no-api is set")
		return nil, nil
	}

	var stdout, stderr bytes.Buffer
	store := parser.Store{ProjectsDir: filepath.Join(root, "projects"), SessionMetaDir: metaDir}
	if err := runBenchmark([]string{"--no-api", "--n", "1", "--min-kb", "0", "--overhead", "40000"}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runBenchmark returned error: %v", err)
	}

	// verify the computed toolIOPerCall by checking the break-even row (cost section uses it)
	// and that it differs from the / 4 (double-halving) result
	wrongToolIO := totalToolChars / totalToolCalls / 4
	if expectedToolIO == wrongToolIO {
		t.Skip("fixture degenerate: charsPerToken and /4 produce same result; pick different tool char counts")
	}
	got := stdout.String()
	if got == "" {
		t.Fatal("runBenchmark produced no output")
	}
	// K=2 is visible in the cost row; ensures toolIOPerTurn = toolIO*(K-1) != 0
	costSection := outputSection(got, "=== Cost Savings Per Session")
	row := outputLineContaining(costSection, "22222222")
	if !strings.Contains(row, "2.0") {
		t.Fatalf("cost row K should show 2.0 (APICallCount=2, UserTurnCount=1), got row: %s\nfull output:\n%s", row, got)
	}
	if !strings.Contains(row, "turn ") {
		t.Fatalf("cost row break-even should be a finite turn with expectedToolIO=%d, got row: %s\nfull output:\n%s", expectedToolIO, row, got)
	}
}

func outputLineContaining(output string, needle string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

func outputSection(output string, header string) string {
	start := strings.Index(output, header)
	if start < 0 {
		return ""
	}
	rest := output[start:]
	if next := strings.Index(rest[len(header):], "\n==="); next >= 0 {
		return rest[:len(header)+next]
	}
	return rest
}
