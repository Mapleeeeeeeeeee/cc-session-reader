// Package inject implements paginated session output for the inject subcommand.
// Each page stays under 20K chars so Claude Code's Bash tool returns it as
// stdout rather than persisting it to a file.
package inject

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/formatter"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/skillpath"
)

const maxPageChars = 20_000

// State tracks pagination progress for one session.
type State struct {
	SessionID  string `json:"session_id"`
	OffsetLine int    `json:"offset_line"`
	TotalLines int    `json:"total_lines"`
	Page       int    `json:"page"`
}

func stateDir() (string, error) {
	dir := filepath.Join(skillpath.SkillDir(), "inject-state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create state dir: %w", err)
	}
	return dir, nil
}

func stateFile(sessionID string) (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	safe := strings.NewReplacer("/", "_", "\\", "_").Replace(sessionID)
	return filepath.Join(dir, safe+".json"), nil
}

// LoadState returns stored pagination state for sessionID, or nil if none exists.
func LoadState(sessionID string) (*State, error) {
	path, err := stateFile(sessionID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	return &s, nil
}

// SaveState persists pagination state.
func SaveState(s State) error {
	path, err := stateFile(s.SessionID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// ClearState removes any stored state for sessionID.
func ClearState(sessionID string) error {
	path, err := stateFile(sessionID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove state: %w", err)
	}
	return nil
}

// SplitPages divides lines into pages whose char count stays under maxPageChars.
// Page breaks always fall on line boundaries.
func SplitPages(lines []string) [][]string {
	if len(lines) == 0 {
		return nil
	}
	var pages [][]string
	var current []string
	currentChars := 0

	for _, line := range lines {
		lineChars := len(line) + 1 // +1 for newline
		if currentChars+lineChars > maxPageChars && len(current) > 0 {
			pages = append(pages, current)
			current = nil
			currentChars = 0
		}
		current = append(current, line)
		currentChars += lineChars
	}
	if len(current) > 0 {
		pages = append(pages, current)
	}
	return pages
}

// RenderFullOutput produces the complete formatted session text without line limits.
func RenderFullOutput(transcriptPath string, reader session.TranscriptReader) (string, error) {
	opts := formatter.FormatOptions{}
	var buf bytes.Buffer
	if err := formatter.FormatRead(transcriptPath, 0, 0, opts, &buf, reader); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// WritePage writes one page to out, including header and footer.
func WritePage(pageLines []string, pageNum, totalPages, startLine, totalLines int, out io.Writer) {
	endLine := startLine + len(pageLines)
	fmt.Fprintf(out, "[page %d/%d | lines %d-%d of %d]\n", pageNum, totalPages, startLine+1, endLine, totalLines)
	fmt.Fprint(out, strings.Join(pageLines, "\n"))
	if len(pageLines) > 0 && pageLines[len(pageLines)-1] != "" {
		fmt.Fprintln(out)
	}
	if pageNum == totalPages {
		fmt.Fprintf(out, "[inject complete: %d pages, %d lines]\n", totalPages, totalLines)
	} else {
		fmt.Fprintf(out, "[page %d/%d complete — run again for next page]\n", pageNum, totalPages)
	}
}
