package claudecodec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTailReadOffset_GivenSizeWithinBudget_ThenReturnsZero(t *testing.T) {
	if got := tailReadOffset(tailReadBytes); got != 0 {
		t.Fatalf("tailReadOffset(%d) = %d, want 0", tailReadBytes, got)
	}
}

func TestTailReadOffset_GivenSizeBeyondBudget_ThenReturnsTrailingWindowStart(t *testing.T) {
	size := int64(tailReadBytes*3 + 17)
	want := size - tailReadBytes
	if got := tailReadOffset(size); got != want {
		t.Fatalf("tailReadOffset(%d) = %d, want %d", size, got, want)
	}
}

func TestReadLastTimestamp_GivenSmallFile_ThenReturnsLastLineTimestamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"user","timestamp":"2026-07-15T02:00:00.000Z","message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","timestamp":"2026-07-15T02:03:00.000Z","message":{"role":"assistant","content":[{"type":"text","text":"hi back"}]}}`,
		"",
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	got, err := readLastTimestamp(f)
	if err != nil {
		t.Fatalf("readLastTimestamp returned error: %v", err)
	}
	if got != "2026-07-15T02:03:00.000Z" {
		t.Fatalf("readLastTimestamp = %q, want the last line's timestamp", got)
	}
}

// TestReadLastTimestamp_GivenTrailingLinesWithoutTimestamp_ThenSkipsBackToPriorTimestampedLine
// guards the backward-scan step: real transcripts end with bridge-session/
// last-prompt entries carrying no "timestamp" field at all, so the tail scan
// must walk past them to the nearest timestamped line rather than giving up.
func TestReadLastTimestamp_GivenTrailingLinesWithoutTimestamp_ThenSkipsBackToPriorTimestampedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"user","timestamp":"2026-07-15T02:00:00.000Z","message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","timestamp":"2026-07-15T02:03:00.000Z","message":{"role":"assistant","content":[{"type":"text","text":"hi back"}]}}`,
		`{"type":"bridge-session","sessionId":"abc"}`,
		`{"type":"last-prompt"}`,
		"",
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	got, err := readLastTimestamp(f)
	if err != nil {
		t.Fatalf("readLastTimestamp returned error: %v", err)
	}
	if got != "2026-07-15T02:03:00.000Z" {
		t.Fatalf("readLastTimestamp = %q, want the last *timestamped* line, skipping trailing entries without one", got)
	}
}

// TestReadLastTimestamp_GivenTimestampOnlyOutsideTailWindow_ThenReturnsEmpty
// is the O(1) regression guard: transcripts can run to many megabytes, and
// `list` scans every session file, so duration computation must not read the
// whole file just to find its last timestamp. Only a decoy line near the
// start of the fixture carries a timestamp; everything inside the true
// trailing tailReadBytes window is timestamp-less noise. A correct
// tail-bounded read never sees the decoy and must return "" — if it did, the
// read walked further back than the documented window, defeating the point
// of bounding duration computation for large transcripts.
func TestReadLastTimestamp_GivenTimestampOnlyOutsideTailWindow_ThenReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")

	decoyLine := `{"type":"user","timestamp":"2020-01-01T00:00:00.000Z","message":{"role":"user","content":"decoy"}}` + "\n"
	noiseLine := `{"type":"noise","note":"no timestamp field here"}` + "\n"
	// Enough noise-only bytes after the decoy to push it outside the tail
	// window, however large tailReadBytes is configured to be.
	noiseTail := strings.Repeat(noiseLine, (tailReadBytes*2)/len(noiseLine)+1)

	content := decoyLine + noiseTail
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	got, err := readLastTimestamp(f)
	if err != nil {
		t.Fatalf("readLastTimestamp returned error: %v", err)
	}
	if got != "" {
		t.Fatalf("readLastTimestamp = %q, want empty (decoy timestamp lies outside the tail window and must not be found)", got)
	}
}
