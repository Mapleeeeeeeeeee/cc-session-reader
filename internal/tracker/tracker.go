// Package tracker records CLI usage events to a local JSONL log and provides
// helpers to detect the calling Claude Code session.
package tracker

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// UsageEntry holds metadata about a single CLI invocation.
type UsageEntry struct {
	Timestamp string `json:"ts"`
	Command   string `json:"cmd"`
	Target    string `json:"target"`
	Cwd       string `json:"cwd"`
	Caller    string `json:"caller"`
}

// DefaultLogPath returns the canonical path for the usage log.
func DefaultLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "skills", "sessions", "usage.jsonl")
}

// LogUsage appends entry to the default log path.
func LogUsage(entry UsageEntry) error {
	return LogUsageToPath(entry, DefaultLogPath())
}

// LogUsageToPath appends entry as a JSON line to path, creating the directory
// and file if needed.
func LogUsageToPath(entry UsageEntry, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = f.Write(line)
	return err
}

// ReadUsageLog reads entries from the default log path.
// limit <= 0 means no limit. cmdFilter is an exact match on the Command field;
// empty string returns all entries.
func ReadUsageLog(limit int, cmdFilter string) ([]UsageEntry, error) {
	return ReadUsageLogFromPath(limit, cmdFilter, DefaultLogPath())
}

// ReadUsageLogFromPath reads and parses the JSONL file at path.
// Returns entries in reverse chronological order (most-recent first).
// If the file does not exist, returns an empty slice and nil error.
// Blank or unparseable lines are silently skipped.
func ReadUsageLogFromPath(limit int, cmdFilter string, path string) ([]UsageEntry, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return []UsageEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []UsageEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e UsageEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if cmdFilter != "" && e.Command != cmdFilter {
			continue
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Reverse to get newest first (file is append-only chronological).
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

// DetectCallerSession maps cwd to the most recently modified session JSONL in
// the matching Claude Code project directory. Returns an empty string if the
// directory does not exist, contains no JSONL files, or any other error occurs.
func DetectCallerSession(cwd string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return DetectCallerSessionWithBase(cwd, filepath.Join(home, ".claude", "projects"))
}

// DetectCallerSessionWithBase is the testable variant of DetectCallerSession
// that accepts an explicit projectsDir.
func DetectCallerSessionWithBase(cwd string, projectsDir string) string {
	// Claude Code maps an absolute path to a project dir by replacing every
	// "/" with "-", e.g. /Users/maple/Desktop -> -Users-maple-Desktop.
	projectDir := filepath.Join(projectsDir, strings.ReplaceAll(cwd, "/", "-"))

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return ""
	}

	type jsonlFile struct {
		name    string
		modTime int64 // UnixNano for fast comparison
	}
	var candidates []jsonlFile
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, jsonlFile{
			name:    e.Name(),
			modTime: info.ModTime().UnixNano(),
		})
	}

	if len(candidates) == 0 {
		return ""
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime > candidates[j].modTime
	})
	return strings.TrimSuffix(candidates[0].name, ".jsonl")
}
