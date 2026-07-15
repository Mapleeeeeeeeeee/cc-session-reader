package formatter

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func FormatRead(transcriptPath string, maxLines int, offset int, opts FormatOptions, out io.Writer, reader session.TranscriptReader) error {
	events, agentIDs, err := loadEvents(transcriptPath, opts.VerboseAgents, reader)
	if err != nil {
		return err
	}
	return FormatReadEvents(events, agentIDs, maxLines, offset, opts, out)
}

func FormatReadEvents(events []session.Event, agentIDs map[string]bool, maxLines int, offset int, opts FormatOptions, out io.Writer) error {
	// Two-pass: format all events into a buffer, then apply offset + maxLines on output lines.
	var buf bytes.Buffer
	if err := renderReadEvents(events, renderContext{agentIDs: agentIDs, opts: opts, out: &buf}); err != nil {
		return err
	}
	return applyPagination(buf.String(), maxLines, offset, out)
}

// RenderReadEventsWithSink renders the full read-format output (no pagination —
// the same text `cc-session inherit` injects via inject.RenderFullOutput) while
// reporting every unit of kept content to sink, tagged by category. This lets
// analyzer.ComputeStats derive its KEPT breakdown from the exact same render
// pass that produces the injected text, instead of a second implementation
// that can silently drift from what read/context actually keep (e.g. cc-session
// call collapsing, which only ever happened here).
func RenderReadEventsWithSink(events []session.Event, agentIDs map[string]bool, opts FormatOptions, sink ContentSink) (string, error) {
	var buf bytes.Buffer
	if err := renderReadEvents(events, renderContext{agentIDs: agentIDs, opts: opts, out: &buf, sink: sink}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderReadEvents writes the full formatted timeline to rc.out without any line limits.
func renderReadEvents(events []session.Event, rc renderContext) error {
	var pendingTools []pendingTool
	seenSkills := make(map[string]bool)

	flush := func() {
		flushPendingTools(&pendingTools, rc)
	}

	for _, event := range events {
		switch event.Kind {
		case session.EventUserMessage:
			rendered := renderUserMessage(event.User, rc.opts, seenSkills)
			if !rendered.show {
				continue
			}
			flush()
			fmt.Fprintf(rc.out, "[%s] user:\n%s\n\n", parser.FormatTimestamp(event.Timestamp), rendered.body)
			if rc.sink != nil {
				rc.sink(CategoryUserText, rendered.body)
			}

		case session.EventAssistantMessage:
			if event.Assistant == nil {
				continue
			}
			if rc.opts.VerboseThinking {
				for _, thinking := range event.Assistant.Thinking {
					flush()
					fmt.Fprintf(rc.out, "[%s] thinking:\n%s\n\n", parser.FormatTimestamp(event.Timestamp), thinking)
				}
			}
			hasText := strings.TrimSpace(event.Assistant.Text) != ""
			hasTools := len(event.Assistant.ToolUses) > 0
			if hasText {
				flush()
				fmt.Fprintf(rc.out, "[%s] assistant:\n%s\n", parser.FormatTimestamp(event.Timestamp), event.Assistant.Text)
				if rc.sink != nil {
					rc.sink(CategoryAssistantText, event.Assistant.Text)
				}
			}
			for _, tool := range event.Assistant.ToolUses {
				pendingTools = append(pendingTools, summarizeToolUse(tool))
			}
			if hasText && !hasTools {
				fmt.Fprintln(rc.out)
			}

		case session.EventToolResult:
			handleToolResultRead(event, &pendingTools, flush, rc)
		}
	}

	flush()
	return nil
}

func handleToolResultRead(event session.Event, pendingTools *[]pendingTool, flushFn func(), rc renderContext) {
	if event.User != nil && event.User.IsAnswer {
		flushFn()
		fmt.Fprintf(rc.out, "[%s] user (answer):\n%s\n\n", parser.FormatTimestamp(event.Timestamp), event.User.Text)
		if rc.sink != nil {
			rc.sink(CategoryUserAnswer, event.User.Text)
		}
		return
	}
	if event.Tool == nil {
		return
	}
	if rc.agentIDs[event.Tool.ToolUseID] && strings.TrimSpace(event.Tool.Text) != "" {
		flushFn()
		fmt.Fprintf(rc.out, "[%s] agent result:\n%s\n\n", parser.FormatTimestamp(event.Timestamp), event.Tool.Text)
		if rc.sink != nil {
			rc.sink(CategoryToolSummary, event.Tool.Text)
		}
		return
	}
	appendToolResult(event.Tool, pendingTools, rc.opts)
}
