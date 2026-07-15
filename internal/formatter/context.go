package formatter

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/jsonutil"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func FormatContextWithStore(transcriptPath string, sessionID string, maxLines int, offset int, opts FormatOptions, out io.Writer, store parser.Store, reader session.TranscriptReader) error {
	events, agentIDs, err := loadEvents(transcriptPath, opts.VerboseAgents, reader)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	writeContextHeader(sessionID, transcriptPath, events, &buf, store)
	if err := renderContextEvents(events, agentIDs, opts, &buf); err != nil {
		return err
	}
	return applyPagination(buf.String(), maxLines, offset, out)
}

func FormatContextEvents(events []session.Event, agentIDs map[string]bool, maxLines int, offset int, opts FormatOptions, out io.Writer) error {
	// Two-pass: format all events into a buffer, then apply offset + maxLines on output lines.
	var buf bytes.Buffer
	if err := renderContextEvents(events, agentIDs, opts, &buf); err != nil {
		return err
	}
	return applyPagination(buf.String(), maxLines, offset, out)
}

// renderContextEvents writes the full compact context format to out without any line limits.
func renderContextEvents(events []session.Event, agentIDs map[string]bool, opts FormatOptions, out io.Writer) error {
	var pendingTools []pendingTool
	seenSkills := make(map[string]bool)

	flush := func() {
		flushPendingTools(&pendingTools, opts, out, nil)
	}

	for _, event := range events {
		switch event.Kind {
		case session.EventUserMessage:
			rendered := renderUserMessage(event.User, opts, seenSkills)
			if !rendered.show {
				continue
			}
			flush()
			fmt.Fprintf(out, "U: %s\n\n", rendered.body)

		case session.EventAssistantMessage:
			if event.Assistant == nil {
				continue
			}
			if opts.VerboseThinking {
				for _, thinking := range event.Assistant.Thinking {
					flush()
					fmt.Fprintf(out, "T: %s\n\n", thinking)
				}
			}
			if strings.TrimSpace(event.Assistant.Text) != "" {
				flush()
				fmt.Fprintf(out, "A: %s\n\n", event.Assistant.Text)
			}
			for _, tool := range event.Assistant.ToolUses {
				pendingTools = append(pendingTools, summarizeToolUse(tool))
			}

		case session.EventToolResult:
			if event.User != nil && event.User.IsAnswer {
				flush()
				fmt.Fprintf(out, "U (answer): %s\n\n", event.User.Text)
				continue
			}
			if event.Tool == nil {
				continue
			}
			if agentIDs[event.Tool.ToolUseID] && strings.TrimSpace(event.Tool.Text) != "" {
				flush()
				fmt.Fprintf(out, "Agent result:\n%s\n\n", event.Tool.Text)
				continue
			}
			appendToolResult(event.Tool, &pendingTools, opts)
		}
	}

	flush()
	return nil
}

// writeContextHeader writes the one-line session header that opens every
// context output. It prefers session_meta (richer: real project path,
// recorded duration) and falls back to a minimal header derived from the
// transcript itself when metadata is unavailable, so a fresh reader always
// gets a session id and project on line 1 instead of raw dialogue.
func writeContextHeader(sessionID string, transcriptPath string, events []session.Event, out io.Writer, store parser.Store) {
	meta, err := store.LoadSessionMeta(sessionID)
	if err == nil && meta != nil {
		writeMetaContextHeader(sessionID, meta, out)
		return
	}
	writeFallbackContextHeader(sessionID, transcriptPath, events, out)
}

func writeMetaContextHeader(sessionID string, meta map[string]any, out io.Writer) {
	projectPath := jsonutil.GetStr(meta, "project_path")
	project := filepath.Base(projectPath)
	if project == "" || project == "." {
		project = "?"
	}
	duration := "?"
	if d, ok := meta["duration_minutes"]; ok {
		duration = fmt.Sprintf("%v", d)
	}
	shortID := session.ShortID(sessionID, 8)
	fmt.Fprintf(out, "# Session %s | %s | %sm\n\n", shortID, project, duration)
}

// writeFallbackContextHeader writes a minimal header sourced from the
// transcript itself: session id, project (from a tool call's recorded cwd,
// or the transcript's parent directory name as a last resort), and the date
// of the first event. Used when session metadata is missing — notably the
// ~/.claude/usage-data/session-meta/ upstream stopped writing new files as of
// 2026-07-04, so without this fallback every recent session's context output
// opened with raw dialogue and no indication of which project or session it
// belonged to.
func writeFallbackContextHeader(sessionID string, transcriptPath string, events []session.Event, out io.Writer) {
	project := projectFromEventCwd(events)
	if project == "" {
		project = filepath.Base(filepath.Dir(transcriptPath))
	}
	if project == "" || project == "." {
		project = "?"
	}
	date := parser.FormatTimestamp(firstEventTimestamp(events))
	shortID := session.ShortID(sessionID, 8)
	fmt.Fprintf(out, "# Session %s | %s | %s (no session metadata)\n\n", shortID, project, date)
}

// firstEventTimestamp returns the first non-empty timestamp among events, in
// order. Leading noise events (mode/permission-mode/bridge-session/...) carry
// no timestamp field, so events[0].Timestamp alone is not reliable.
func firstEventTimestamp(events []session.Event) string {
	for _, event := range events {
		if event.Timestamp != "" {
			return event.Timestamp
		}
	}
	return ""
}

// projectFromEventCwd returns the directory name of the first recorded tool
// call's working directory, or "" if no event carries one.
func projectFromEventCwd(events []session.Event) string {
	for _, event := range events {
		if event.Assistant == nil {
			continue
		}
		for _, tool := range event.Assistant.ToolUses {
			if tool.Cwd != "" {
				return filepath.Base(tool.Cwd)
			}
		}
	}
	return ""
}
