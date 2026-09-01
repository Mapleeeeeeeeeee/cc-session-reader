package session

import "testing"

// ADR-007 decision 1: before it, a successful result's excerpt was the first
// non-empty line with no filtering, so `gh`'s usage banner was reported as the
// state of a pull request and a script's own "--- HEAD ---" as the state of a
// working tree. These cases are the real transcript lines that motivated the
// change; each one guards against the excerpt reverting to line 1.

func TestSuccessExcerpt_GivenNoisyFirstLine_WhenSummarized_ThenSkipsToTheAnswer(t *testing.T) {
	tests := map[string]struct {
		output string
		want   string
	}{
		"a script's own section heading labels the answer below it": {
			output: "--- HEAD ---\n8fe4ebc feat: add archetype rules",
			want:   "8fe4ebc feat: add archetype rules",
		},
		"a heading closed by its own rule is skipped, a diff header is not": {
			output: "=== branch 落後/超前 ===\nahead 3, behind 0",
			want:   "ahead 3, behind 0",
		},
		"a progress line announces work rather than reporting it": {
			output: "Checking formatting...\nAll matched files use Prettier code style!",
			want:   "All matched files use Prettier code style!",
		},
		"a bracketed status prefix is progress, not outcome": {
			output: "[STARTED] Backing up original state...\nBackup complete: 42 files",
			want:   "Backup complete: 42 files",
		},
		"a test runner's version banner precedes the result": {
			output: "RUN v4.0.18\nTest Files  23 passed (23)",
			want:   "Test Files  23 passed (23)",
		},
		"terminal escape sequences are stripped from the excerpt": {
			output: "\x1b[32m✓\x1b[39m 23 passed",
			want:   "✓ 23 passed",
		},
		"a banner wrapped in escape sequences is still recognized as one": {
			output: "\x1b[1m\x1b[46m RUN \x1b[49m \x1b[36mv4.0.18\x1b[39m\nTest Files  23 passed (23)",
			want:   "Test Files  23 passed (23)",
		},
		"an ordinary first line is kept": {
			output: "65:  CAMPUS_BUS_AND_HOUSING: 'campus-bus-and-housing',\nmore output",
			want:   "65:  CAMPUS_BUS_AND_HOUSING: 'campus-bus-and-housing',",
		},
		// A diff's "--- a/file.go" opens with the same rule as a section
		// heading but is not closed by one, so it must survive as the excerpt.
		"a diff header is not mistaken for a section heading": {
			output: "--- a/src/game-engine.ts\n+++ b/src/game-engine.ts",
			want:   "--- a/src/game-engine.ts",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			result := ToolResult{Success: true, RawName: ToolBash, Text: tc.output}
			want := " -> " + tc.want
			if got := result.Summary(); got != want {
				t.Errorf("Summary() = %q, want %q", got, want)
			}
		})
	}
}

// A prose usage banner carries no shape to key on, so pattern filtering
// cannot reach it. This pins the limitation so a future reader does not
// assume ADR-007 decision 1 covers every misleading excerpt: `gh`'s banner is
// addressed by decision 2 instead, which puts "gh pr view" in the same line
// and lets the reader see that the banner is not the answer.
func TestSuccessExcerpt_GivenProseUsageBanner_WhenSummarized_ThenStillReportsIt(t *testing.T) {
	result := ToolResult{
		Success: true,
		RawName: ToolBash,
		Text:    "Work seamlessly with GitHub from the command line.\n{\"state\":\"success\"}",
	}

	if got, want := result.Summary(), " -> Work seamlessly with GitHub from the command line."; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
}

func TestSuccessExcerpt_GivenEveryLineIsNoise_WhenSummarized_ThenKeepsTheFirstLineRatherThanDroppingIt(t *testing.T) {
	result := ToolResult{Success: true, RawName: ToolBash, Text: "=== 規模 ===\n--- HEAD ---"}

	if got, want := result.Summary(), " -> === 規模 ==="; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
}

// The success filter must not leak into the failure path: ADR-004 keeps
// failure information, and a line that reads as a banner in passing output can
// be the error itself when the call failed.
func TestFailureExcerpt_GivenSuccessOnlyNoiseShapes_WhenSummarized_ThenKeepsThemAsTheError(t *testing.T) {
	tests := map[string]string{
		"a version-shaped line can be the failure itself": "expected v1.2.3, got v1.2.4",
		"a heading-shaped line can be the failure itself": "=== FAILED: 3 assertions ===",
	}

	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			result := ToolResult{Success: false, RawName: ToolBash, Text: output}
			want := " -> FAILED: " + output
			if got := result.Summary(); got != want {
				t.Errorf("Summary() = %q, want %q", got, want)
			}
		})
	}
}

func TestFailureExcerpt_GivenEscapeSequences_WhenSummarized_ThenStripsThem(t *testing.T) {
	result := ToolResult{Success: false, RawName: ToolBash, Text: "\x1b[31m× Scenario: a date with trust at 100\x1b[39m"}

	if got, want := result.Summary(), " -> FAILED: × Scenario: a date with trust at 100"; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
}
