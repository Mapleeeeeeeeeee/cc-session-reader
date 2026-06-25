package parser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LoadSessionMeta reads session metadata from the store's session-meta directory.
func (s Store) LoadSessionMeta(sessionID string) (map[string]any, error) {
	metaFile := filepath.Join(s.SessionMetaDir, sessionID+".json")
	data, err := os.ReadFile(metaFile)
	if err != nil {
		return nil, err
	}
	data = SanitizeMetaJSON(data)
	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse session meta %s: %w", sessionID, err)
	}
	return meta, nil
}

// SanitizeMetaJSON cleans raw session metadata JSON bytes to handle two
// common on-disk corruption patterns:
//
//  1. Null-byte padding: valid JSON followed by \x00 bytes filling a
//     pre-allocated block.  bytes.TrimRight strips them before parsing.
//  2. Truncated JSON: valid JSON cut mid-field due to an incomplete write.
//     repairTruncatedJSON attempts best-effort recovery by locating the last
//     complete key-value pair and balancing unclosed braces/brackets.
//
// If neither fix produces valid JSON the original trimmed bytes are returned,
// letting json.Unmarshal surface the underlying parse error to the caller.
func SanitizeMetaJSON(data []byte) []byte {
	trimmed := bytes.TrimRight(data, "\x00")
	if len(trimmed) == 0 {
		return trimmed
	}
	if json.Valid(trimmed) {
		return trimmed
	}
	return repairTruncatedJSON(trimmed)
}

// repairTruncatedJSON attempts to recover a JSON object that was cut short
// during a write.  It scans the input byte-by-byte with a minimal state
// machine (string / escape / depth tracking) and records the furthest
// position at which the partial document could be cleanly truncated.
// After truncation it appends the closing delimiters needed to balance any
// open braces or brackets.
//
// If the repaired result is not valid JSON the original data is returned
// unchanged.
func repairTruncatedJSON(data []byte) []byte {
	type safePoint struct {
		end   int    // byte index (exclusive) to cut at
		stack []byte // snapshot of open delimiters at this cut point
	}

	var (
		stack      []byte // stack of open '{' or '[' characters
		inString   bool
		escapeNext bool
		best       safePoint // last known-good cut position
	)

	snapshotStack := func() []byte {
		cp := make([]byte, len(stack))
		copy(cp, stack)
		return cp
	}

	for i := 0; i < len(data); i++ {
		b := data[i]

		if escapeNext {
			escapeNext = false
			continue
		}

		if inString {
			switch b {
			case '\\':
				escapeNext = true
			case '"':
				inString = false
			}
			continue
		}

		switch b {
		case '"':
			inString = true

		case '{', '[':
			stack = append(stack, b)

		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if len(stack) == 0 {
				// Closed the outermost delimiter — valid complete object.
				best = safePoint{end: i + 1, stack: snapshotStack()}
			}

		case ',':
			// A comma at depth 1 means we just finished a key-value pair.
			// Truncating here (before the comma) and closing the open brace
			// yields a valid object containing all pairs up to this point.
			if len(stack) == 1 {
				best = safePoint{end: i, stack: snapshotStack()}
			}
		}
	}

	// Try the full remaining data as a candidate (captures complete final
	// values that appear after the last comma but before truncation, e.g.
	// {"a":"b","c":5  →  best from comma is just {"a":"b"} but closing at
	// len(data) gives {"a":"b","c":5} which is valid once braces are added).
	if !inString && len(stack) >= 1 {
		endCandidate := safePoint{end: len(data), stack: snapshotStack()}
		trial := make([]byte, endCandidate.end, endCandidate.end+len(endCandidate.stack))
		copy(trial, data[:endCandidate.end])
		for j := len(endCandidate.stack) - 1; j >= 0; j-- {
			switch endCandidate.stack[j] {
			case '{':
				trial = append(trial, '}')
			case '[':
				trial = append(trial, ']')
			}
		}
		if json.Valid(trial) {
			return trial
		}
	}

	// No safe truncation point found; nothing to salvage.
	if best.end == 0 {
		return data
	}

	// Build the candidate: truncated prefix + closing delimiters in reverse
	// stack order.
	candidate := make([]byte, best.end, best.end+len(best.stack))
	copy(candidate, data[:best.end])
	for j := len(best.stack) - 1; j >= 0; j-- {
		switch best.stack[j] {
		case '{':
			candidate = append(candidate, '}')
		case '[':
			candidate = append(candidate, ']')
		}
	}

	if json.Valid(candidate) {
		return candidate
	}
	return data
}

// SessionMetaFile holds metadata about a session, used for listing.
type SessionMetaFile struct {
	Path    string
	ModTime time.Time
}

// ListSessionMetaFiles returns session meta files sorted by modification time (newest first).
func (s Store) ListSessionMetaFiles() ([]SessionMetaFile, error) {
	entries, err := os.ReadDir(s.SessionMetaDir)
	if err != nil {
		return nil, fmt.Errorf("read session meta dir: %w", err)
	}

	var files []SessionMetaFile
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, SessionMetaFile{
			Path:    filepath.Join(s.SessionMetaDir, e.Name()),
			ModTime: info.ModTime(),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.After(files[j].ModTime)
	})
	return files, nil
}

func parseISO(s string) (time.Time, error) {
	s = strings.Replace(s, "Z", "+00:00", 1)
	formats := []string{
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05.000-07:00",
		"2006-01-02T15:04:05.000000-07:00",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable timestamp: %s", s)
}

// sessionMetaJSON is the on-disk shape of a session metadata file.
// Mirrors listSessionMeta in cmd/sessions/main.go; kept here so ListAllSessions
// can read metadata without coupling to the cmd package.
type sessionMetaJSON struct {
	SessionID             string `json:"session_id"`
	ProjectPath           string `json:"project_path"`
	StartTime             string `json:"start_time"`
	DurationMinutes       int    `json:"duration_minutes"`
	UserMessageCount      int    `json:"user_message_count"`
	AssistantMessageCount int    `json:"assistant_message_count"`
	FirstPrompt           string `json:"first_prompt"`
}
