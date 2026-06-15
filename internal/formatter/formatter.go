// Package formatter handles read and context output formatting.
package formatter

import (
	"bytes"
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

// FormatOptions controls verbosity for formatting functions.
type FormatOptions struct {
	VerboseAgents   bool
	VerboseBash     bool
	VerboseThinking bool
	VerboseCommands bool
}

// userRender is the rendered form of a user-message event: the body to print
// and whether anything should be printed at all.
type userRender struct {
	body string
	show bool
}

// renderUserMessage resolves how a user-message event should appear given the
// verbosity options. It is the single rendering policy shared by read and
// context so both stay consistent:
//   - command invocation -> always show the marker (e.g. "[/context]")
//   - command noise -> drop by default; show ANSI-stripped body under
//     -verbose-commands, except caveats which are always dropped
//   - plain typed message -> show verbatim
func renderUserMessage(user *session.UserMessage, opts FormatOptions) userRender {
	if user == nil {
		return userRender{}
	}
	if user.CommandMarker != "" {
		return userRender{body: user.CommandMarker, show: true}
	}
	if user.IsCommandNoise {
		if !opts.VerboseCommands || user.IsCaveat {
			return userRender{}
		}
		body := strings.TrimSpace(session.StripANSI(user.Text))
		if body == "" {
			return userRender{}
		}
		return userRender{body: body, show: true}
	}
	if strings.TrimSpace(user.Text) == "" {
		return userRender{}
	}
	return userRender{body: user.Text, show: true}
}

type pendingTool struct {
	summary string
	name    string // e.g. "Bash", "Read", "Edit"
}

func FormatRead(transcriptPath string, maxLines int, offset int, opts FormatOptions, out io.Writer) error {
	events, agentIDs, err := loadEvents(transcriptPath, opts.VerboseAgents)
	if err != nil {
		return err
	}
	return FormatReadEvents(events, agentIDs, maxLines, offset, opts, out)
}

func FormatReadEvents(events []session.Event, agentIDs map[string]bool, maxLines int, offset int, opts FormatOptions, out io.Writer) error {
	// Two-pass: format all events into a buffer, then apply offset + maxLines on output lines.
	var buf bytes.Buffer
	if err := renderReadEvents(events, agentIDs, opts, &buf); err != nil {
		return err
	}
	return applyPagination(buf.String(), maxLines, offset, out)
}

// renderReadEvents writes the full formatted timeline to out without any line limits.
func renderReadEvents(events []session.Event, agentIDs map[string]bool, opts FormatOptions, out io.Writer) error {
	var pendingTools []pendingTool

	flush := func() {
		for _, pt := range pendingTools {
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
			rendered := renderUserMessage(event.User, opts)
			if !rendered.show {
				continue
			}
			flush()
			fmt.Fprintf(out, "[%s] user:\n%s\n\n", parser.FormatTimestamp(event.Timestamp), rendered.body)

		case session.EventAssistantMessage:
			if event.Assistant == nil {
				continue
			}
			if opts.VerboseThinking {
				for _, thinking := range event.Assistant.Thinking {
					flush()
					fmt.Fprintf(out, "[%s] thinking:\n%s\n\n", parser.FormatTimestamp(event.Timestamp), thinking)
				}
			}
			hasText := strings.TrimSpace(event.Assistant.Text) != ""
			hasTools := len(event.Assistant.ToolUses) > 0
			if hasText {
				flush()
				fmt.Fprintf(out, "[%s] assistant:\n%s\n", parser.FormatTimestamp(event.Timestamp), event.Assistant.Text)
			}
			for _, tool := range event.Assistant.ToolUses {
				pendingTools = append(pendingTools, summarizeToolUse(tool))
			}
			if hasText && !hasTools {
				fmt.Fprintln(out)
			}

		case session.EventToolResult:
			handleToolResultRead(event, agentIDs, &pendingTools, opts, flush, out)
		}
	}

	flush()
	return nil
}

// applyPagination slices the formatted output by offset and maxLines, writing
// the result to out. It appends a truncation message when lines were cut.
func applyPagination(formatted string, maxLines int, offset int, out io.Writer) error {
	allLines := strings.Split(formatted, "\n")
	// strings.Split on a trailing newline produces an empty last element; exclude it
	// from the count so line math matches what the user sees.
	totalLines := len(allLines)
	if totalLines > 0 && allLines[totalLines-1] == "" {
		totalLines--
	}

	if offset >= totalLines {
		if totalLines > 0 {
			fmt.Fprintf(out, "--- offset %d exceeds total ~%d lines ---\n", offset, totalLines)
		}
		return nil
	}

	visibleLines := allLines[offset:]
	isTruncated := false
	if maxLines > 0 && len(visibleLines) > maxLines {
		visibleLines = visibleLines[:maxLines]
		isTruncated = true
	}

	fmt.Fprint(out, strings.Join(visibleLines, "\n"))
	// Restore the trailing newline that strings.Split consumed, unless the last
	// visible line is already empty (which would produce a double newline).
	lastVisible := visibleLines[len(visibleLines)-1]
	if lastVisible != "" {
		fmt.Fprintln(out)
	}

	if isTruncated {
		resumeAt := offset + maxLines
		fmt.Fprintf(out, "\n--- truncated at line %d (total ~%d lines) — use --offset %d to continue ---\n", resumeAt, totalLines, resumeAt)
	}
	return nil
}

func FormatContextWithStore(transcriptPath string, sessionID string, maxLines int, offset int, opts FormatOptions, out io.Writer, store parser.Store) error {
	events, agentIDs, err := loadEvents(transcriptPath, opts.VerboseAgents)
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

	flush := func() {
		for _, pt := range pendingTools {
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
			rendered := renderUserMessage(event.User, opts)
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

func handleToolResultRead(
	event session.Event,
	agentIDs map[string]bool,
	pendingTools *[]pendingTool,
	opts FormatOptions,
	flushFn func(),
	out io.Writer,
) {
	if event.User != nil && event.User.IsAnswer {
		flushFn()
		fmt.Fprintf(out, "[%s] user (answer):\n%s\n\n", parser.FormatTimestamp(event.Timestamp), event.User.Text)
		return
	}
	if event.Tool == nil {
		return
	}
	if agentIDs[event.Tool.ToolUseID] && strings.TrimSpace(event.Tool.Text) != "" {
		flushFn()
		fmt.Fprintf(out, "[%s] agent result:\n%s\n\n", parser.FormatTimestamp(event.Timestamp), event.Tool.Text)
		return
	}
	appendToolResult(event.Tool, pendingTools, opts)
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

func appendToolResult(result *session.ToolResult, pendingTools *[]pendingTool, opts FormatOptions) {
	if len(*pendingTools) > 0 {
		last := &(*pendingTools)[len(*pendingTools)-1]
		if opts.VerboseBash && last.name == session.ToolBash {
			last.summary += formatVerboseBashResult(result)
			return
		}
		last.summary += result.Summary()
		return
	}
	name := result.RawName
	if name == "" {
		name = "ToolResult"
	}
	summary := fmt.Sprintf("[%s]%s", name, result.Summary())
	if opts.VerboseBash && name == session.ToolBash {
		summary = fmt.Sprintf("[%s]%s", name, formatVerboseBashResult(result))
	}
	*pendingTools = append(*pendingTools, pendingTool{
		summary: summary,
		name:    name,
	})
}

func formatVerboseBashResult(result *session.ToolResult) string {
	text := strings.TrimSpace(result.Text)
	if text == "" {
		return fmt.Sprintf(" -> %s", result.Status())
	}
	indented := indentBlock(text, "    ")
	return fmt.Sprintf(" -> %s:\n%s", result.Status(), indented)
}

func indentBlock(text string, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

func summarizeToolUse(tool session.ToolUse) pendingTool {
	name := tool.Name
	if name == "" {
		name = "?"
	}
	shortID := session.ToolShortID(tool.ID)
	summary := summarizer.SummarizeToolUse(name, tool.Input)
	// Inject "#shortID" before the closing ']' of the first bracket group
	// so "[Bash] cmd" becomes "[Bash#ol-1] cmd" and
	// "[Agent(general)] desc" becomes "[Agent(general)#ol-1] desc".
	tagged := injectShortID(summary, shortID)
	return pendingTool{
		summary: tagged,
		name:    name,
	}
}

// injectShortID inserts "#id" before the first ']' in summary.
// "[Bash] Run tests" -> "[Bash#uCVa] Run tests"
// "[Agent(general)] Inspect" -> "[Agent(general)#uCVa] Inspect"
func injectShortID(summary string, shortID string) string {
	if shortID == "" {
		return summary
	}
	idx := strings.Index(summary, "]")
	if idx < 0 {
		return summary
	}
	return summary[:idx] + "#" + shortID + summary[idx:]
}
