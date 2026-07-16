package claudecodec

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
)

// tailReadBytes bounds how many trailing bytes of a transcript are read to
// locate its last timestamp. Transcripts can run to many megabytes, and
// `list` scans every session file on every invocation, so seeking straight
// to a small tail window keeps duration computation O(1) instead of
// O(file size).
const tailReadBytes = 16 * 1024

// tailReadOffset returns the byte offset to seek to before reading the tail
// of a file of the given size, so at most tailReadBytes trailing bytes are
// ever read.
func tailReadOffset(size int64) int64 {
	if size <= tailReadBytes {
		return 0
	}
	return size - tailReadBytes
}

// readLastTimestamp scans backward through the trailing tailReadBytes of f
// for the last JSONL entry carrying a non-empty "timestamp" field, skipping
// entries (bridge-session, last-prompt, ...) that carry none. Returns ""
// with a nil error if the tail window contains no timestamped entry.
func readLastTimestamp(f *os.File) (string, error) {
	stat, err := f.Stat()
	if err != nil {
		return "", err
	}

	offset := tailReadOffset(stat.Size())
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}
	chunk, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}

	lines := bytes.Split(chunk, []byte("\n"))
	if offset > 0 && len(lines) > 0 {
		// Seeking into the middle of the file lands mid-record: the first
		// fragment is a partial line and must be discarded rather than
		// parsed as JSON.
		lines = lines[1:]
	}

	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		var h struct {
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal(line, &h); err != nil {
			continue
		}
		if h.Timestamp != "" {
			return h.Timestamp, nil
		}
	}
	return "", nil
}
