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
	writeContextHeader(sessionID, &buf, store)
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
		tools := pendingTools
		if !opts.VerboseBash {
			tools = collapseCCSessionTools(tools)
		}
		for _, pt := range tools {
			fmt.Fprintf(out, "  %s\n", pt.summary)
		}
		if len(pendingTools) > 0 {
			fmt.Fprintln(out)
		}
		pendingTools = pendingTools[:0]
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

func writeContextHeader(sessionID string, out io.Writer, store parser.Store) {
	meta, err := store.LoadSessionMeta(sessionID)
	if err != nil || meta == nil {
		return
	}
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
