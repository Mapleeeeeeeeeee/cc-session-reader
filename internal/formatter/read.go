package formatter

import (
	"bytes"
	"fmt"
	"io"
	"strings"

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
	rc.ts = &timestampWriter{}

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
			writeEventBlock(rc, event.Timestamp, "user", rendered.body, true)
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
					writeEventBlock(rc, event.Timestamp, "thinking", thinking, true)
				}
			}
			hasText := strings.TrimSpace(event.Assistant.Text) != ""
			hasTools := len(event.Assistant.ToolUses) > 0
			if hasText {
				flush()
				writeEventBlock(rc, event.Timestamp, "assistant", event.Assistant.Text, false)
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
		writeEventBlock(rc, event.Timestamp, "user (answer)", event.User.Text, true)
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
		writeEventBlock(rc, event.Timestamp, "agent result", event.Tool.Text, true)
		if rc.sink != nil {
			rc.sink(CategoryToolSummary, event.Tool.Text)
		}
		return
	}
	appendToolResult(event.Tool, pendingTools, rc.opts)
}

// writeEventBlock prints one "[time] role:" block. A day marker precedes it
// whenever the date has rolled over since the previous block, which is where
// the date lives now that the per-message header carries only a clock time.
// trailingBlank is false for assistant text, which may be followed by its own
// tool-call block and supplies the separating blank line itself.
func writeEventBlock(rc renderContext, timestamp string, role string, body string, trailingBlank bool) {
	label, dayMarker := rc.ts.format(timestamp)
	if dayMarker != "" {
		fmt.Fprintf(rc.out, "%s\n\n", dayMarker)
	}
	fmt.Fprintf(rc.out, "[%s] %s:\n%s\n", label, role, body)
	if trailingBlank {
		fmt.Fprintln(rc.out)
	}
}
