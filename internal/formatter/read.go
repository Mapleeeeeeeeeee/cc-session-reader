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
	if err := renderReadEvents(events, agentIDs, opts, &buf, nil); err != nil {
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
	if err := renderReadEvents(events, agentIDs, opts, &buf, sink); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderReadEvents writes the full formatted timeline to out without any line limits.
func renderReadEvents(events []session.Event, agentIDs map[string]bool, opts FormatOptions, out io.Writer, sink ContentSink) error {
	var pendingTools []pendingTool
	seenSkills := make(map[string]bool)

	flush := func() {
		flushPendingTools(&pendingTools, opts, out, sink)
	}

	for _, event := range events {
		switch event.Kind {
		case session.EventUserMessage:
			rendered := renderUserMessage(event.User, opts, seenSkills)
			if !rendered.show {
				continue
			}
			flush()
			fmt.Fprintf(out, "[%s] user:\n%s\n\n", parser.FormatTimestamp(event.Timestamp), rendered.body)
			if sink != nil {
				sink(CategoryUserText, rendered.body)
			}

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
				if sink != nil {
					sink(CategoryAssistantText, event.Assistant.Text)
				}
			}
			for _, tool := range event.Assistant.ToolUses {
				pendingTools = append(pendingTools, summarizeToolUse(tool))
			}
			if hasText && !hasTools {
				fmt.Fprintln(out)
			}

		case session.EventToolResult:
			handleToolResultRead(event, agentIDs, &pendingTools, opts, flush, out, sink)
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
	sink ContentSink,
) {
	if event.User != nil && event.User.IsAnswer {
		flushFn()
		fmt.Fprintf(out, "[%s] user (answer):\n%s\n\n", parser.FormatTimestamp(event.Timestamp), event.User.Text)
		if sink != nil {
			sink(CategoryUserAnswer, event.User.Text)
		}
		return
	}
	if event.Tool == nil {
		return
	}
	if agentIDs[event.Tool.ToolUseID] && strings.TrimSpace(event.Tool.Text) != "" {
		flushFn()
		fmt.Fprintf(out, "[%s] agent result:\n%s\n\n", parser.FormatTimestamp(event.Timestamp), event.Tool.Text)
		if sink != nil {
			sink(CategoryToolSummary, event.Tool.Text)
		}
		return
	}
	appendToolResult(event.Tool, pendingTools, opts)
}
