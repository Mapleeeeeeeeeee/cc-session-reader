package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

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

// ScanTranscriptHeaders walks ProjectsDir for .jsonl files and extracts a
// SessionListEntry from a bounded prefix of each file via the Store's
// HeaderScanner, with DurationMinutes computed from the scanner's first and
// last transcript timestamps. Files that cannot be opened or parsed are
// silently skipped. Sessions with the same UUID in multiple project
// directories are deduplicated (first walk hit wins). Returns nil when
// ProjectsDir is empty or no HeaderScanner is configured.
func (s Store) ScanTranscriptHeaders() []SessionListEntry {
	if s.ProjectsDir == "" || s.HeaderScanner == nil {
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

		header, scanErr := s.HeaderScanner.ScanHeader(path)
		if scanErr != nil {
			return nil
		}

		entry := SessionListEntry{
			SessionID:   sessionID,
			ProjectPath: filepath.Base(filepath.Dir(path)),
			FromMeta:    false,
		}

		if header.Timestamp != "" {
			entry.StartTime = header.Timestamp
			if t, err := parseISO(header.Timestamp); err == nil {
				entry.StartTimeParsed = t
				if header.EndTimestamp != "" {
					if end, err := parseISO(header.EndTimestamp); err == nil {
						if elapsed := end.Sub(t); elapsed >= 0 {
							entry.DurationMinutes = int(elapsed.Minutes())
						}
					}
				}
			}
		}

		if header.FirstUserPrompt != "" {
			entry.FirstPrompt = truncateRunes(header.FirstUserPrompt, 80)
		}

		seen[sessionID] = true
		entries = append(entries, entry)
		return nil
	})

	return entries
}

// truncateRunes truncates s to at most maxRunes runes, appending "..." if truncation occurs.
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes-3]) + "..."
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

		var startParsed time.Time
		if t, err := parseISO(m.StartTime); err == nil {
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

	// --- filter out subagent sessions ---
	filtered := all[:0]
	for _, e := range all {
		if strings.HasPrefix(e.SessionID, "agent-") {
			continue
		}
		e.FirstPrompt = sanitizeFirstPrompt(e.FirstPrompt)
		filtered = append(filtered, e)
	}
	all = filtered

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

func sanitizeFirstPrompt(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if idx := strings.Index(s, "<"); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	return truncateRunes(s, 80)
}
