package analyzer

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// --- FormatNumber ---

func TestFormatNumber_GivenSmallNumber_WhenFormatted_ThenNoCommas(t *testing.T) {
	cases := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{999, "999"},
		{-5, "-5"},
		{-999, "-999"},
	}
	for _, c := range cases {
		got := FormatNumber(c.input)
		if got != c.expected {
			t.Errorf("FormatNumber(%d) = %q, want %q", c.input, got, c.expected)
		}
	}
}

func TestFormatNumber_GivenLargeNumber_WhenFormatted_ThenCommasSeparateThousands(t *testing.T) {
	cases := []struct {
		input    int
		expected string
	}{
		{1000, "1,000"},
		{1234, "1,234"},
		{1234567, "1,234,567"},
		{1000000000, "1,000,000,000"},
		{-1234567, "-1,234,567"},
	}
	for _, c := range cases {
		got := FormatNumber(c.input)
		if got != c.expected {
			t.Errorf("FormatNumber(%d) = %q, want %q", c.input, got, c.expected)
		}
	}
}

// --- PrintConfigHint ---

func TestPrintConfigHint_GivenWriter_WhenCalled_ThenOutputsAPIKeyHint(t *testing.T) {
	var buf bytes.Buffer
	PrintConfigHint(&buf)
	out := buf.String()

	if !strings.Contains(out, filepath.Join(".claude", "skills", "cc-session", "config.json")) {
		t.Errorf("PrintConfigHint output missing config path, got: %q", out)
	}
	if !strings.Contains(out, "anthropic_api_key_file") {
		t.Errorf("PrintConfigHint output missing anthropic_api_key_file key, got: %q", out)
	}
}

// --- RenderStats helpers ---

func baseResult() StatsResult {
	return StatsResult{
		RawChars:      1000,
		FilteredChars: 600,
		Categories: map[string]int{
			"user_text":       100,
			"user_answers":    50,
			"assistant_text":  200,
			"tool_summaries":  250,
			"tool_input_raw":  200,
			"tool_result_raw": 150,
			"system_noise":    30,
			"command_noise":   20,
		},
		PerTool: map[string]*ToolStats{
			"Bash": {CallCount: 10, InputChars: 500, ResultChars: 3000},
			"Read": {CallCount: 5, InputChars: 100, ResultChars: 800},
		},
		LastContextTokens: 2000,
		TotalOutputTokens: 500,
		APICallCount:      8,
	}
}

func baseOpts() RenderOptions {
	return RenderOptions{
		TranscriptKB:   42.5,
		SessionID:      "session-abc",
		FilteredTokens: 1200,
		RawTokens:      1800,
		HasAPIData:     true,
		TokenErr:       nil,
	}
}

// --- RenderStats: Characters section ---

func TestRenderStats_GivenFullData_WhenRendered_ThenCharactersSectionPresent(t *testing.T) {
	var out, errOut bytes.Buffer
	RenderStats(&out, &errOut, baseResult(), baseOpts())
	body := out.String()

	assertOutputContains(t, body, "=== Characters ===")
	assertOutputLineContaining(t, body, "Raw:", "1,000")    // RawChars anchored to label
	assertOutputLineContaining(t, body, "Filtered:", "600") // FilteredChars anchored to label
	assertOutputContains(t, body, "40.0%") // (1000-600)/1000 = 40%
}

// --- RenderStats: Breakdown section ---

func TestRenderStats_GivenFullData_WhenRendered_ThenBreakdownSectionPresent(t *testing.T) {
	var out, errOut bytes.Buffer
	RenderStats(&out, &errOut, baseResult(), baseOpts())
	body := out.String()

	assertOutputContains(t, body, "=== Breakdown ===")
	assertOutputContains(t, body, "KEPT  user text:")
	assertOutputContains(t, body, "CUT   tool input (raw):")
}

// --- RenderStats: Per-tool section ---

func TestRenderStats_GivenMultipleTools_WhenRendered_ThenPerToolSectionSortedByTotalDescending(t *testing.T) {
	var out, errOut bytes.Buffer
	RenderStats(&out, &errOut, baseResult(), baseOpts())
	body := out.String()

	assertOutputContains(t, body, "=== Per-tool ===")

	bashIdx := strings.Index(body, "Bash")
	readIdx := strings.Index(body, "Read")
	if bashIdx == -1 || readIdx == -1 {
		t.Fatalf("expected both Bash and Read in output, got: %q", body)
	}
	// Bash total = 3500, Read total = 900 → Bash should appear first
	if bashIdx > readIdx {
		t.Errorf("expected Bash (total 3500) to appear before Read (total 900) in per-tool output")
	}
}

func TestRenderStats_GivenNoTools_WhenRendered_ThenPerToolSectionAbsent(t *testing.T) {
	result := baseResult()
	result.PerTool = nil
	var out, errOut bytes.Buffer
	RenderStats(&out, &errOut, result, baseOpts())

	if strings.Contains(out.String(), "=== Per-tool ===") {
		t.Error("expected Per-tool section to be absent when PerTool is nil")
	}
}

// --- RenderStats: Model Context section ---

func TestRenderStats_GivenAPICallCount_WhenRendered_ThenModelContextSectionPresent(t *testing.T) {
	var out, errOut bytes.Buffer
	RenderStats(&out, &errOut, baseResult(), baseOpts())
	body := out.String()

	assertOutputContains(t, body, "=== Model Context (from API usage) ===")
	assertOutputLineContaining(t, body, "Last turn context:", "2,000")
	assertOutputLineContaining(t, body, "Total output:", "500")
	assertOutputLineContaining(t, body, "API calls:", "8") // APICallCount anchored to label
}

func TestRenderStats_GivenZeroAPICallCount_WhenRendered_ThenModelContextSectionAbsent(t *testing.T) {
	result := baseResult()
	result.APICallCount = 0
	var out, errOut bytes.Buffer
	RenderStats(&out, &errOut, result, baseOpts())

	if strings.Contains(out.String(), "=== Model Context") {
		t.Error("expected Model Context section to be absent when APICallCount is 0")
	}
}

// --- RenderStats: Token Savings section ---

func TestRenderStats_GivenHasAPIDataAndNoError_WhenRendered_ThenTokenSavingsSectionPresent(t *testing.T) {
	var out, errOut bytes.Buffer
	RenderStats(&out, &errOut, baseResult(), baseOpts())
	body := out.String()

	assertOutputContains(t, body, "=== Token Savings ===")
	assertOutputContains(t, body, "2,000") // Original context (LastContextTokens)
	assertOutputContains(t, body, "1,200") // CLI filtered
	assertOutputContains(t, body, "40.0%") // (2000-1200)/2000 = 40%
}

func TestRenderStats_GivenSkipTokens_WhenRendered_ThenTokenSectionAbsent(t *testing.T) {
	opts := baseOpts()
	opts.SkipTokens = true
	var out, errOut bytes.Buffer
	RenderStats(&out, &errOut, baseResult(), opts)
	body := out.String()

	if strings.Contains(body, "Token") {
		t.Errorf("expected no token section when SkipTokens=true, got: %q", body)
	}
}

func TestRenderStats_GivenHasAPIDataWithTokenError_WhenRendered_ThenConfigHintWrittenToOut(t *testing.T) {
	opts := baseOpts()
	opts.HasAPIData = true
	opts.TokenErr = errors.New("unauthorized")
	var out, errOut bytes.Buffer
	RenderStats(&out, &errOut, baseResult(), opts)

	if !strings.Contains(out.String(), "anthropic_api_key_file") {
		t.Errorf("expected config hint in out when HasAPIData=true and TokenErr set, got: %q", out.String())
	}
	if strings.Contains(errOut.String(), "anthropic_api_key_file") {
		t.Errorf("config hint must not appear in errOut (would render red in PowerShell), got: %q", errOut.String())
	}
	if strings.Contains(out.String(), "=== Token Savings ===") {
		t.Error("expected Token Savings section to be absent when TokenErr is set")
	}
}

func TestRenderStats_GivenNoAPIDataAndTokenCounts_WhenRendered_ThenAnthropicAPITokenSectionPresent(t *testing.T) {
	opts := baseOpts()
	opts.HasAPIData = false
	opts.RawTokens = 1800
	opts.FilteredTokens = 900
	var out, errOut bytes.Buffer
	RenderStats(&out, &errOut, baseResult(), opts)
	body := out.String()

	assertOutputContains(t, body, "=== Tokens (Anthropic API) ===")
	assertOutputContains(t, body, fmt.Sprintf("  Raw:      %10s\n", FormatNumber(1800)))
	assertOutputContains(t, body, fmt.Sprintf("  Filtered: %10s\n", FormatNumber(900)))
	assertOutputContains(t, body, fmt.Sprintf("  Saved:    %10s (50.0%%)\n", FormatNumber(900)))
}

func TestRenderStats_GivenNoAPIDataWithTokenError_WhenRendered_ThenConfigHintWrittenToOut(t *testing.T) {
	opts := baseOpts()
	opts.HasAPIData = false
	opts.TokenErr = errors.New("no key")
	var out, errOut bytes.Buffer
	RenderStats(&out, &errOut, baseResult(), opts)

	// hint goes to stdout to avoid red text in PowerShell
	if !strings.Contains(out.String(), "anthropic_api_key_file") {
		t.Errorf("expected config hint in out when no API data and TokenErr set, got: %q", out.String())
	}
	if strings.Contains(errOut.String(), "anthropic_api_key_file") {
		t.Errorf("config hint must not appear in errOut (would render red in PowerShell), got: %q", errOut.String())
	}
}

// --- RenderStats: session header ---

func TestRenderStats_GivenSessionID_WhenRendered_ThenHeaderContainsSessionIDAndTranscriptKB(t *testing.T) {
	var out, errOut bytes.Buffer
	RenderStats(&out, &errOut, baseResult(), baseOpts())
	body := out.String()

	assertOutputContains(t, body, "Session: session-abc")
	assertOutputContains(t, body, "Transcript: 42.5KB")
}

// assertOutputLineContaining finds the first line in s containing lineLabel and
// fails the test if that line does not also contain value. This prevents false
// positives where value appears in unrelated fields on other lines.
func assertOutputLineContaining(t *testing.T, s, lineLabel, value string) {
	t.Helper()
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, lineLabel) {
			if !strings.Contains(line, value) {
				t.Errorf("line containing %q should also contain %q, got: %s", lineLabel, value, line)
			}
			return
		}
	}
	t.Errorf("no line containing %q found in output:\n%s", lineLabel, s)
}

// assertOutputContains fails the test if substr is not found in s.
func assertOutputContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected output to contain %q\ngot:\n%s", substr, s)
	}
}
