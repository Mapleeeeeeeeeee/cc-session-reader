package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func cmdExpand(args []string, reader session.TranscriptReader) {
	exitOnError(runExpand(args, os.Stdout, os.Stderr, parser.DefaultStore(), reader))
}

func runExpand(args []string, out io.Writer, errOut io.Writer, store parser.Store, reader session.TranscriptReader) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: cc-session expand <session-id> <tool-id> [tool-id...]\n\n  tool-id is the short ID shown in read output, e.g. [Grep#Q1hv] → Q1hv\n\n  example: cc-session expand abc12345 Q1hv\n          cc-session expand abc12345 Q1hv ooQF xY3z\n\n  tip: to expand all tool calls of a type, use read -verbose-bash / -verbose-agents")
	}

	sessionPrefix := args[0]
	requestedIDs := args[1:] // short IDs to expand

	resolved, err := store.ResolveSession(sessionPrefix)
	if err != nil {
		return err
	}
	if resolved.Path == "" {
		return fmt.Errorf("transcript not found: %s", resolved.ID)
	}
	logUsageAsync("expand", session.ShortID(resolved.ID, 8), requestedIDs...)

	events, err := reader.ReadAll(resolved.Path)
	if err != nil {
		return fmt.Errorf("parsing transcript: %w", err)
	}

	// Build maps: shortID -> []ToolUse (collisions collected, not overwritten),
	// full toolUseID -> ToolResult.
	// Short IDs are only the last 4 chars of tool_use_id, so collisions are
	// common in long sessions. Collecting all matches lets us detect a collision
	// and refuse to guess, instead of silently returning the last one written.
	toolUsesByShortID := make(map[string][]session.ToolUse)
	toolResults := make(map[string]session.ToolResult)

	for _, event := range events {
		if event.Assistant != nil {
			for _, tu := range event.Assistant.ToolUses {
				shortID := session.ToolShortID(tu.ID)
				toolUsesByShortID[shortID] = append(toolUsesByShortID[shortID], tu)
			}
		}
		if event.Tool != nil {
			toolResults[event.Tool.ToolUseID] = *event.Tool
		}
	}

	// Expand each requested ID
	found := 0
	for _, reqID := range requestedIDs {
		candidates := matchToolUses(toolUsesByShortID, reqID)
		if len(candidates) == 0 {
			fmt.Fprintf(errOut, "warning: tool ID %s not found\n", reqID)
			continue
		}
		if len(candidates) > 1 {
			fmt.Fprintf(errOut, "warning: tool ID %s is ambiguous (matches %d tools); disambiguate with a longer/full tool_use_id:\n", reqID, len(candidates))
			for _, c := range candidates {
				fmt.Fprintf(errOut, "  %s\n", c.ID)
			}
			continue
		}
		tu := candidates[0]
		found++

		fmt.Fprintf(out, "=== [%s#%s] ===\n", tu.Name, reqID)
		fmt.Fprintf(out, "Input:\n")
		fmt.Fprintf(out, "  %s\n", tu.Input.MarshalNoEscape())

		if result, ok := toolResults[tu.ID]; ok {
			fmt.Fprintf(out, "Result (%s):\n", result.Status())
			if result.Text != "" {
				fmt.Fprintf(out, "%s\n", result.Text)
			}
		}
		fmt.Fprintln(out)
	}

	if found == 0 {
		return fmt.Errorf("no matching tool IDs found. Use 'cc-session read <session-id>' to see available IDs")
	}
	return nil
}

// matchToolUses resolves a user-requested tool ID to the matching tool uses.
// A request matching a short ID (last 4 chars) returns every tool use sharing
// that short ID; the caller treats >1 match as an ambiguous collision. A
// request longer than a short ID is treated as a full/partial tool_use_id and
// matched by suffix so users can disambiguate a collision with a longer ID.
func matchToolUses(byShortID map[string][]session.ToolUse, reqID string) []session.ToolUse {
	candidates := byShortID[session.ToolShortID(reqID)]
	if len(reqID) <= 4 {
		return candidates
	}
	var matched []session.ToolUse
	for _, tu := range candidates {
		if strings.HasSuffix(tu.ID, reqID) {
			matched = append(matched, tu)
		}
	}
	return matched
}
