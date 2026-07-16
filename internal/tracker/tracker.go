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

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/skillpath"
)

// UsageEntry holds metadata about a single CLI invocation.
type UsageEntry struct {
	Timestamp string `json:"ts"`
	Command   string `json:"cmd"`
	Target    string `json:"target"`
	Cwd       string `json:"cwd"`
	Caller    string `json:"caller"`
	Version   string `json:"version,omitempty"`
	Commit    string `json:"commit,omitempty"`
	// Result is "ok" or "error", recorded once the invocation finishes.
	// Empty on entries written before this field existed (and on entries
	// for commands that don't track a result) — treat as unknown, not as
	// a failure.
	Result string `json:"result,omitempty"`
	// Error holds the first line of the failing command's error message.
	// Empty unless Result == "error".
	Error string `json:"error,omitempty"`
	// ToolIDs holds the short tool IDs requested by an "expand" invocation
	// (e.g. the "Q1hv" in [Grep#Q1hv]), so usage analysis can tell what a
	// caller wanted to inspect. Empty for every other command, and for
	// entries written before this field existed.
	ToolIDs []string `json:"tool_ids,omitempty"`
}

// commandAliases maps a deprecated subcommand name recorded by older
// binaries to its current name, so usage queries and stats treat historical
// entries the same as new ones. "inject" was renamed to "inherit" in
// 554e57b; see commands.go's hidden "inject" registry entry.
var commandAliases = map[string]string{
	"inject": "inherit",
}

// canonicalCommand resolves cmd through commandAliases, returning cmd
// unchanged if it has no alias.
func canonicalCommand(cmd string) string {
	if canon, ok := commandAliases[cmd]; ok {
		return canon
	}
	return cmd
}

// DefaultLogPath returns the canonical path for the usage log.
// Returns an empty string and an error if the home directory is unavailable.
func DefaultLogPath() (string, error) {
	dir, err := skillpath.SkillDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "usage.jsonl"), nil
}

// LogUsage appends entry to the default log path.
// Returns nil silently if the home directory is unavailable.
func LogUsage(entry UsageEntry) error {
	path, err := DefaultLogPath()
	if err != nil {
		return nil
	}
	return LogUsageToPath(entry, path)
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
// Returns an empty slice silently if the home directory is unavailable.
// limit <= 0 means no limit. cmdFilter is an exact match on the Command field;
// empty string returns all entries.
func ReadUsageLog(limit int, cmdFilter string) ([]UsageEntry, error) {
	path, err := DefaultLogPath()
	if err != nil {
		return []UsageEntry{}, nil
	}
	return ReadUsageLogFromPath(limit, cmdFilter, path)
}

// ReadUsageLogFromPath reads and parses the JSONL file at path.
// Returns entries in reverse chronological order (most-recent first).
// If the file does not exist, returns an empty slice and nil error.
// Blank or unparseable lines are silently skipped. cmdFilter and each
// entry's Command are both resolved through canonicalCommand first, so a
// deprecated alias (e.g. "inject") matches its current name ("inherit") in
// either direction, and the returned entries always display the current name.
func ReadUsageLogFromPath(limit int, cmdFilter string, path string) ([]UsageEntry, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return []UsageEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cmdFilter = canonicalCommand(cmdFilter)

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
		e.Command = canonicalCommand(e.Command)
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

// CallerSessionIDs reads the usage log and returns a set of caller session IDs
// that have invoked cc-session (i.e., sessions that reference other sessions).
// The returned map keys are full caller UUIDs. Returns an empty map on any error.
func CallerSessionIDs() map[string]bool {
	path, err := DefaultLogPath()
	if err != nil {
		return make(map[string]bool)
	}
	return CallerSessionIDsFromPath(path)
}

// CallerSessionIDsFromPath is the testable variant.
func CallerSessionIDsFromPath(path string) map[string]bool {
	result := make(map[string]bool)
	f, err := os.Open(path)
	if err != nil {
		return result
	}
	defer f.Close()

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
		if e.Caller != "" && e.Target != "" {
			result[e.Caller] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return make(map[string]bool)
	}
	return result
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

// ProjectDirName maps an absolute working directory path to the directory
// name Claude Code uses under ~/.claude/projects, by replacing every path
// separator with "-" (e.g. /Users/maple/Desktop -> -Users-maple-Desktop).
// A Windows cwd uses "\" as its separator and may carry a drive letter
// (e.g. "C:"); both "\" and ":" are illegal inside a single path segment,
// so they are normalized the same way as "/" — otherwise the mapped name
// would embed an OS-illegal segment (e.g. "D:") that os.MkdirAll refuses to
// create. This is a no-op for a macOS/Linux cwd, which never contains
// "\" or ":".
func ProjectDirName(cwd string) string {
	return strings.NewReplacer("\\", "-", "/", "-", ":", "-").Replace(cwd)
}

// DetectCallerSessionWithBase is the testable variant of DetectCallerSession
// that accepts an explicit projectsDir.
func DetectCallerSessionWithBase(cwd string, projectsDir string) string {
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	projectDir := filepath.Join(projectsDir, ProjectDirName(cwd))

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
