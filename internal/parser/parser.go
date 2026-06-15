// Package parser handles session discovery and metadata I/O.
package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Store points at Claude Code's on-disk session data.
type Store struct {
	ProjectsDir    string
	SessionMetaDir string
}

// DefaultStore returns a Store derived from the current user's ~/.claude.
func DefaultStore() Store {
	claudeDir := filepath.Join(homeDir(), ".claude")
	return Store{
		ProjectsDir:    filepath.Join(claudeDir, "projects"),
		SessionMetaDir: filepath.Join(claudeDir, "usage-data", "session-meta"),
	}
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// FindTranscript locates a transcript JSONL file by session ID under the store's projects dir.
func (s Store) FindTranscript(sessionID string) (string, error) {
	var found string
	err := filepath.Walk(s.ProjectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if base == sessionID+".jsonl" {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk projects dir: %w", err)
	}
	return found, nil
}

// ResolvedSession holds the session ID and transcript path resolved in a single walk.
type ResolvedSession struct {
	ID   string
	Path string
}

// ResolveSession resolves a prefix to a full session UUID and its transcript path
// in a single filesystem walk.
func (s Store) ResolveSession(prefix string) (ResolvedSession, error) {
	// Reject an empty prefix up front. Without this, an empty string is treated
	// as a prefix that matches every session, yielding a misleading "ambiguous
	// prefix ''" error when the user simply forgot to supply an ID. This is the
	// single choke point for all commands that accept a session_id (read,
	// context, stats, audit via resolveSession; expand calls this directly).
	if prefix == "" {
		return ResolvedSession{}, fmt.Errorf("session_id is required")
	}
	if len(prefix) == 36 {
		path, err := s.FindTranscript(prefix)
		if err != nil {
			return ResolvedSession{}, err
		}
		return ResolvedSession{ID: prefix, Path: path}, nil
	}

	type match struct {
		id   string
		path string
	}
	var matches []match
	err := filepath.Walk(s.ProjectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".jsonl" {
			stem := strings.TrimSuffix(filepath.Base(path), ".jsonl")
			if strings.HasPrefix(stem, prefix) {
				matches = append(matches, match{id: stem, path: path})
			}
		}
		return nil
	})
	if err != nil {
		return ResolvedSession{}, fmt.Errorf("walk projects dir: %w", err)
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].id < matches[j].id })

	// A single UUID can live in multiple project dirs (worktrees reuse the session
	// ID), producing several matches with an identical stem. Dedup by UUID before
	// judging single vs. ambiguous so those copies are not mistaken for a conflict.
	// We keep the first walk match per UUID, matching the full-UUID fast path which
	// takes the first walk hit.
	seen := make(map[string]bool)
	uniqueMatches := matches[:0:0]
	for _, m := range matches {
		if seen[m.id] {
			continue
		}
		seen[m.id] = true
		uniqueMatches = append(uniqueMatches, m)
	}

	if len(uniqueMatches) == 1 {
		return ResolvedSession{ID: uniqueMatches[0].id, Path: uniqueMatches[0].path}, nil
	}
	if len(uniqueMatches) > 1 {
		shown := uniqueMatches
		if len(shown) > 5 {
			shown = shown[:5]
		}
		shortIDs := make([]string, len(shown))
		for i, m := range shown {
			if len(m.id) >= 12 {
				shortIDs[i] = m.id[:12]
			} else {
				shortIDs[i] = m.id
			}
		}
		return ResolvedSession{}, fmt.Errorf("ambiguous prefix '%s', matches: %s", prefix, strings.Join(shortIDs, ", "))
	}
	return ResolvedSession{}, fmt.Errorf("session prefix not found: %s", prefix)
}

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

// SessionListEntry unifies metadata-based and JSONL-based sessions for the list command.
type SessionListEntry struct {
	SessionID             string
	ProjectPath           string
	StartTime             string    // ISO 8601 timestamp string
	StartTimeParsed       time.Time // parsed for sorting
	DurationMinutes       int
	UserMessageCount      int
	AssistantMessageCount int
	FirstPrompt           string
	FromMeta              bool // true if from metadata, false if from JSONL fallback
}

// commandTagPrefixes are JSONL user message content prefixes that indicate
// tool/command noise rather than real user prompts. Filtered when extracting
// FirstPrompt from transcripts.
var commandTagPrefixes = []string{
	"<command-name>",
	"<local-command-stdout>",
	"<bash-input>",
	"<bash-stdout>",
	"<bash-stderr>",
	"<local-command-caveat>",
}

// jsonlHeaderEntry is a minimal struct for parsing the first lines of a JSONL
// transcript. Avoids importing claudecodec to keep dependency graph simple.
type jsonlHeaderEntry struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// ScanTranscriptHeaders walks ProjectsDir for .jsonl files and extracts a
// SessionListEntry from the first 20 lines of each file. Files that cannot be
// opened or parsed are silently skipped. Sessions with the same UUID in multiple
// project directories are deduplicated (first walk hit wins).
func (s Store) ScanTranscriptHeaders() []SessionListEntry {
	if s.ProjectsDir == "" {
		return nil
	}

	seen := make(map[string]bool)
	var entries []SessionListEntry

	_ = filepath.Walk(s.ProjectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}

		sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		if seen[sessionID] {
			return nil
		}

		entry, ok := scanJSONLHeader(path, sessionID, filepath.Base(filepath.Dir(path)))
		if !ok {
			return nil
		}

		seen[sessionID] = true
		entries = append(entries, entry)
		return nil
	})

	return entries
}

// scanJSONLHeader reads up to 20 lines of a JSONL file and extracts timestamp
// and first user prompt. Returns (entry, true) on success, (zero, false) if the
// file cannot be opened.
func scanJSONLHeader(path, sessionID, projectDir string) (SessionListEntry, bool) {
	f, err := os.Open(path)
	if err != nil {
		return SessionListEntry{}, false
	}
	defer f.Close()

	entry := SessionListEntry{
		SessionID:   sessionID,
		ProjectPath: projectDir,
		FromMeta:    false,
	}

	scanner := bufio.NewScanner(f)
	linesRead := 0
	for scanner.Scan() && linesRead < 20 {
		linesRead++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var h jsonlHeaderEntry
		if err := json.Unmarshal(line, &h); err != nil {
			continue
		}

		if entry.StartTime == "" && h.Timestamp != "" {
			ts := strings.Replace(h.Timestamp, "Z", "+00:00", 1)
			entry.StartTime = h.Timestamp
			if t, err := parseISO(ts); err == nil {
				entry.StartTimeParsed = t
			}
		}

		if entry.FirstPrompt == "" && h.Message != nil && h.Message.Role == "user" && h.Type == "user" {
			var text string
			if err := json.Unmarshal(h.Message.Content, &text); err == nil {
				if !hasCommandTagPrefix(text) {
					entry.FirstPrompt = truncateRunes(text, 80)
				}
			}
		}

		if entry.StartTime != "" && entry.FirstPrompt != "" {
			break
		}
	}

	return entry, true
}

// hasCommandTagPrefix reports whether s begins with any of the known command
// noise prefixes that should not be surfaced as FirstPrompt.
func hasCommandTagPrefix(s string) bool {
	for _, prefix := range commandTagPrefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

// truncateRunes truncates s to at most maxRunes runes, appending "..." if truncation occurs.
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes-3]) + "..."
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

// ListAllSessions returns every discoverable session by merging metadata files
// (authoritative) with JSONL transcript headers (fallback for sessions whose
// metadata has not yet been flushed). Metadata entries always win when both
// sources contain the same session ID.
//
// The returned entries are sorted newest-first by StartTimeParsed; entries with
// zero (unparseable) timestamps sort after all timestamped entries.
//
// The second return value contains warning messages for metadata files that
// could not be read or parsed; these are non-fatal and the remaining entries
// are still returned.
func (s Store) ListAllSessions() ([]SessionListEntry, []string) {
	var warnings []string

	// --- metadata entries (authoritative) ---
	metaFiles, _ := s.ListSessionMetaFiles()
	metaEntries := make(map[string]SessionListEntry)
	for _, mf := range metaFiles {
		data, err := os.ReadFile(mf.Path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("warning: skipping unreadable metadata %s: %v", mf.Path, err))
			continue
		}
		data = SanitizeMetaJSON(data)
		var m sessionMetaJSON
		if err := json.Unmarshal(data, &m); err != nil {
			warnings = append(warnings, fmt.Sprintf("warning: skipping invalid metadata %s: %v", mf.Path, err))
			continue
		}

		sid := m.SessionID
		if sid == "" {
			sid = strings.TrimSuffix(filepath.Base(mf.Path), ".json")
		}

		ts := strings.Replace(m.StartTime, "Z", "+00:00", 1)
		var startParsed time.Time
		if t, err := parseISO(ts); err == nil {
			startParsed = t
		}

		metaEntries[sid] = SessionListEntry{
			SessionID:             sid,
			ProjectPath:           m.ProjectPath,
			StartTime:             m.StartTime,
			StartTimeParsed:       startParsed,
			DurationMinutes:       m.DurationMinutes,
			UserMessageCount:      m.UserMessageCount,
			AssistantMessageCount: m.AssistantMessageCount,
			FirstPrompt:           truncateRunes(m.FirstPrompt, 80),
			FromMeta:              true,
		}
	}

	// --- JSONL fallback entries ---
	transcriptEntries := s.ScanTranscriptHeaders()

	// --- merge: metadata wins, JSONL fills gaps ---
	all := make([]SessionListEntry, 0, len(metaEntries)+len(transcriptEntries))
	for _, e := range metaEntries {
		all = append(all, e)
	}
	for _, e := range transcriptEntries {
		if !metaEntries[e.SessionID].FromMeta {
			all = append(all, e)
		}
	}

	// --- sort newest first; zero timestamps sort last ---
	sort.Slice(all, func(i, j int) bool {
		ti, tj := all[i].StartTimeParsed, all[j].StartTimeParsed
		iZero, jZero := ti.IsZero(), tj.IsZero()
		if iZero != jZero {
			return jZero // non-zero before zero
		}
		if iZero {
			return false // both zero: stable (preserve append order)
		}
		return ti.After(tj)
	})

	return all, warnings
}
