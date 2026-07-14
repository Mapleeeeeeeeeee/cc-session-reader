package inject_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/inject"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

// --- SplitPages ---

func TestGivenEmptyLines_WhenSplitPages_ThenNilSlice(t *testing.T) {
	pages := inject.SplitPages(nil)
	if pages != nil {
		t.Fatalf("expected nil, got %v", pages)
	}
}

func TestGivenLinesUnderLimit_WhenSplitPages_ThenSinglePage(t *testing.T) {
	lines := []string{"hello", "world"}
	pages := inject.SplitPages(lines)
	if len(pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages))
	}
	if len(pages[0]) != 2 {
		t.Fatalf("expected 2 lines in page 0, got %d", len(pages[0]))
	}
}

func TestGivenLinesExceedingLimit_WhenSplitPages_ThenMultiplePages(t *testing.T) {
	// Build content that would exceed 20K chars across two lines
	bigLine := strings.Repeat("x", 15_000)
	lines := []string{bigLine, bigLine}
	pages := inject.SplitPages(lines)
	if len(pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages))
	}
	if len(pages[0]) != 1 || len(pages[1]) != 1 {
		t.Fatalf("each page should have exactly 1 line, got p0=%d p1=%d", len(pages[0]), len(pages[1]))
	}
}

func TestGivenSingleHugeLineLargerThanLimit_WhenSplitPages_ThenOnePageContainsIt(t *testing.T) {
	// A single line larger than max must still be emitted (can't break mid-line)
	bigLine := strings.Repeat("y", 25_000)
	pages := inject.SplitPages([]string{bigLine})
	if len(pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages))
	}
	if pages[0][0] != bigLine {
		t.Fatal("page content mismatch")
	}
}

func TestGivenManySmallLines_WhenSplitPages_ThenNoPagesExceedLimit(t *testing.T) {
	// 1000 lines of 100 chars each = 100K total, should produce at least 5 pages
	line := strings.Repeat("a", 100)
	var lines []string
	for i := 0; i < 1000; i++ {
		lines = append(lines, line)
	}
	pages := inject.SplitPages(lines)
	for i, page := range pages {
		charCount := 0
		for _, l := range page {
			charCount += len(l) + 1
		}
		// Allow a single oversized line to push past, but normally must be under.
		if charCount > 21_000 {
			t.Errorf("page %d has %d chars, exceeds limit", i, charCount)
		}
	}
	// All lines must be preserved
	total := 0
	for _, page := range pages {
		total += len(page)
	}
	if total != 1000 {
		t.Fatalf("expected 1000 total lines across pages, got %d", total)
	}
}

// --- State management ---

func TestGivenNoState_WhenLoadState_ThenNil(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state, err := inject.LoadState("nonexistent-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != nil {
		t.Fatalf("expected nil state, got %+v", state)
	}
}

func TestGivenSavedState_WhenLoadState_ThenMatchesSaved(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saved := inject.State{
		SessionID:  "abc123",
		OffsetLine: 300,
		TotalLines: 1500,
		Page:       1,
	}
	if err := inject.SaveState(saved); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	loaded, err := inject.LoadState("abc123")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil state after save")
	}
	if *loaded != saved {
		t.Fatalf("state mismatch: got %+v, want %+v", *loaded, saved)
	}
}

func TestGivenSavedState_WhenClearState_ThenLoadReturnsNil(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := inject.State{SessionID: "to-clear", OffsetLine: 10, TotalLines: 100, Page: 0}
	if err := inject.SaveState(s); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if err := inject.ClearState("to-clear"); err != nil {
		t.Fatalf("ClearState: %v", err)
	}
	loaded, err := inject.LoadState("to-clear")
	if err != nil {
		t.Fatalf("LoadState after clear: %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected nil after clear, got %+v", loaded)
	}
}

func TestGivenNoExistingState_WhenClearState_ThenNoError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Should not error when nothing to remove
	if err := inject.ClearState("never-saved"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGivenMultipleSessions_WhenSaveAndLoad_ThenStatesAreIndependent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s1 := inject.State{SessionID: "sess-one", OffsetLine: 50, TotalLines: 200, Page: 1}
	s2 := inject.State{SessionID: "sess-two", OffsetLine: 100, TotalLines: 400, Page: 2}
	if err := inject.SaveState(s1); err != nil {
		t.Fatal(err)
	}
	if err := inject.SaveState(s2); err != nil {
		t.Fatal(err)
	}
	loaded1, _ := inject.LoadState("sess-one")
	loaded2, _ := inject.LoadState("sess-two")
	if *loaded1 != s1 {
		t.Errorf("sess-one mismatch: got %+v", *loaded1)
	}
	if *loaded2 != s2 {
		t.Errorf("sess-two mismatch: got %+v", *loaded2)
	}
}

// --- WritePage ---

func TestGivenMiddlePage_WhenWritePage_ThenContinueFooterAppears(t *testing.T) {
	var sb strings.Builder
	inject.WritePage([]string{"line1", "line2"}, 1, 3, 0, 100, &sb)
	out := sb.String()
	if !strings.Contains(out, "[page 1/3 | lines 1-2 of 100]") {
		t.Errorf("missing header in: %q", out)
	}
	if !strings.Contains(out, "[page 1/3 complete — run again for next page]") {
		t.Errorf("missing continue footer in: %q", out)
	}
	if strings.Contains(out, "inherit complete") {
		t.Errorf("should not have complete footer for middle page")
	}
}

func TestGivenLastPage_WhenWritePage_ThenCompleteFooterAppears(t *testing.T) {
	var sb strings.Builder
	inject.WritePage([]string{"final line"}, 3, 3, 90, 100, &sb)
	out := sb.String()
	if !strings.Contains(out, "[page 3/3 | lines 91-91 of 100]") {
		t.Errorf("missing header in: %q", out)
	}
	if !strings.Contains(out, "[inherit complete: 3 pages, 100 lines]") {
		t.Errorf("missing complete footer in: %q", out)
	}
	if !strings.Contains(out, "use -reset to start over") {
		t.Errorf("missing -reset hint in complete footer: %q", out)
	}
}

func TestGivenOnlyOnePage_WhenWritePage_ThenCompleteFooterAppears(t *testing.T) {
	var sb strings.Builder
	inject.WritePage([]string{"only line"}, 1, 1, 0, 1, &sb)
	out := sb.String()
	if !strings.Contains(out, "[inherit complete: 1 pages, 1 lines]") {
		t.Errorf("missing complete footer in: %q", out)
	}
	if !strings.Contains(out, "use -reset to start over") {
		t.Errorf("missing -reset hint in complete footer: %q", out)
	}
}

// --- RenderFullOutput (integration using temp file) ---

func TestGivenInvalidPath_WhenRenderFullOutput_ThenError(t *testing.T) {
	_, err := inject.RenderFullOutput("/nonexistent/path.jsonl", stubReader{})
	if err == nil {
		t.Fatal("expected error for nonexistent transcript")
	}
}

func TestGivenEmptyTranscript_WhenRenderFullOutput_ThenSucceeds(t *testing.T) {
	f, err := os.CreateTemp("", "inject-test-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.Close()

	out, err := inject.RenderFullOutput(f.Name(), claudecodec.Codec{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = out
}

// stubReader always errors — used to exercise error paths in RenderFullOutput.
type stubReader struct{}

func (stubReader) ReadAll(path string) ([]session.Event, error) {
	return nil, fmt.Errorf("stub: open %s: no such file or directory", path)
}
