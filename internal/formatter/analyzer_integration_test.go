package formatter_test

import (
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/analyzer"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
)

// This file lives in the external formatter_test package (not formatter)
// specifically because analyzer.ComputeStats now calls into formatter to
// derive its Filtered measurement from the real read pipeline (see
// analyzer/stats.go). formatter's own tests (package formatter) cannot
// import analyzer without an import cycle; formatter_test can, since it is
// a separate package that only depends on both, never the reverse.

// TestIntegration_FullPipeline_GivenFixture_WhenStatsComputed_ThenCompressionOccurredAndCategoriesPopulated
// verifies that ComputeStats reflects the compression the formatter performs:
// raw text is larger than filtered, and the key categories are non-zero.
func TestIntegration_FullPipeline_GivenFixture_WhenStatsComputed_ThenCompressionOccurredAndCategoriesPopulated(t *testing.T) {
	events, err := claudecodec.Codec{}.ReadAll("testdata/integration.jsonl")
	if err != nil {
		t.Fatalf("read integration fixture: %v", err)
	}

	stats := analyzer.ComputeStats(events)

	if stats.RawChars <= stats.FilteredChars {
		t.Errorf("expected raw chars > filtered chars (compression happened), got raw=%d filtered=%d",
			stats.RawChars, stats.FilteredChars)
	}
	if stats.Categories["user_text"] == 0 {
		t.Errorf("user_text category must be non-zero")
	}
	if stats.Categories["assistant_text"] == 0 {
		t.Errorf("assistant_text category must be non-zero")
	}
	if stats.Categories["tool_summaries"] == 0 {
		t.Errorf("tool_summaries category must be non-zero")
	}
}
