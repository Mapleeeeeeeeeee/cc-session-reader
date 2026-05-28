// Package formatter handles read and context output formatting.
package formatter

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/jsonutil"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/summarizer"
)

func FormatRead(transcriptPath string, maxLines int, isVerboseAgents bool, out io.Writer) error {
	events, agentIDs, err := loadEvents(transcriptPath, isVerboseAgents)
	if err != nil {
		return err
	}
	return FormatReadEvents(events, agentIDs, maxLines, out)
}

func FormatReadEvents(events []session.Event, agentIDs map[string]bool, maxLines int, out io.Writer) error {
	linesOutput := 0
	var pendingTools []string

	flush := func() {
		for _, s := range pendingTools {
			fmt.Fprintf(out, "  %s\n", s)
			linesOutput++
		}
		if len(pendingTools) > 0 {
			fmt.Fprintln(out)
			linesOutput++
		}
		pendingTools = pendingTools[:0]
	}

	for _, event := range events {
		if maxLines > 0 && linesOutput >= maxLines {
			fmt.Fprintf(out, "\n--- truncated at %d output lines ---\n", maxLines)
			break
		}

		switch event.Kind {
		case session.EventUserMessage:
			if event.User == nil || strings.TrimSpace(event.User.Text) == "" {
				continue
			}
			flush()
			fmt.Fprintf(out, "[%s] user:\n%s\n\n", parser.FormatTimestamp(event.Timestamp), event.User.Text)
			linesOutput += strings.Count(event.User.Text, "\n") + 3

		case session.EventAssistantMessage:
			if event.Assistant == nil {
				continue
			}
			hasText := strings.TrimSpace(event.Assistant.Text) != ""
			hasTools := len(event.Assistant.ToolUses) > 0
			if hasText {
				flush()
				fmt.Fprintf(out, "[%s] assistant:\n%s\n", parser.FormatTimestamp(event.Timestamp), event.Assistant.Text)
				linesOutput += strings.Count(event.Assistant.Text, "\n") + 2
			}
			for _, tool := range event.Assistant.ToolUses {
				pendingTools = append(pendingTools, summarizeToolUse(tool))
			}
			if hasText && !hasTools {
				fmt.Fprintln(out)
				linesOutput++
			}

		case session.EventToolResult:
			handleToolResultRead(event, agentIDs, &pendingTools, flush, out, &linesOutput)
		}
	}

	flush()
	return nil
}

func FormatContextWithStore(transcriptPath string, sessionID string, isVerboseAgents bool, out io.Writer, store parser.Store) error {
	events, agentIDs, err := loadEvents(transcriptPath, isVerboseAgents)
	if err != nil {
		return err
	}

	writeContextHeader(sessionID, out, store)
	return FormatContextEvents(events, agentIDs, out)
}

func FormatContextEvents(events []session.Event, agentIDs map[string]bool, out io.Writer) error {
	var pendingTools []string

	flush := func() {
		for _, s := range pendingTools {
			fmt.Fprintf(out, "  %s\n", s)
		}
		if len(pendingTools) > 0 {
			fmt.Fprintln(out)
		}
		pendingTools = pendingTools[:0]
	}

	for _, event := range events {
		switch event.Kind {
		case session.EventUserMessage:
			if event.User == nil || strings.TrimSpace(event.User.Text) == "" {
				continue
			}
			flush()
			fmt.Fprintf(out, "U: %s\n\n", event.User.Text)

		case session.EventAssistantMessage:
			if event.Assistant == nil {
				continue
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
			appendToolResult(event.Tool, &pendingTools)
		}
	}

	flush()
	return nil
}

func handleToolResultRead(
	event session.Event,
	agentIDs map[string]bool,
	pendingTools *[]string,
	flushFn func(),
	out io.Writer,
	linesOutput *int,
) {
	if event.User != nil && event.User.IsAnswer {
		flushFn()
		fmt.Fprintf(out, "[%s] user (answer):\n%s\n\n", parser.FormatTimestamp(event.Timestamp), event.User.Text)
		*linesOutput += strings.Count(event.User.Text, "\n") + 3
		return
	}
	if event.Tool == nil {
		return
	}
	if agentIDs[event.Tool.ToolUseID] && strings.TrimSpace(event.Tool.Text) != "" {
		flushFn()
		fmt.Fprintf(out, "[%s] agent result:\n%s\n\n", parser.FormatTimestamp(event.Timestamp), event.Tool.Text)
		*linesOutput += strings.Count(event.Tool.Text, "\n") + 3
		return
	}
	appendToolResult(event.Tool, pendingTools)
}

func loadEvents(transcriptPath string, isVerboseAgents bool) ([]session.Event, map[string]bool, error) {
	events, err := claudecodec.ReadAll(transcriptPath)
	if err != nil {
		return nil, nil, err
	}
	agentIDs := map[string]bool{}
	if isVerboseAgents {
		agentIDs = claudecodec.CollectAgentToolIDs(events)
	}
	return events, agentIDs, nil
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

func appendToolResult(result *session.ToolResult, pendingTools *[]string) {
	if len(*pendingTools) > 0 {
		(*pendingTools)[len(*pendingTools)-1] += result.Summary()
		return
	}
	name := result.RawName
	if name == "" {
		name = "ToolResult"
	}
	*pendingTools = append(*pendingTools, fmt.Sprintf("[%s]%s", name, result.Summary()))
}

func summarizeToolUse(tool session.ToolUse) string {
	name := tool.Name
	if name == "" {
		name = "?"
	}
	return summarizer.SummarizeToolUse(name, tool.Input)
}
