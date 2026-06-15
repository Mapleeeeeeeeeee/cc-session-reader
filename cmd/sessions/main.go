// Package main is the CLI entry point for the Claude session reader.
// Subcommands: list, read, context, stats, audit, expand, usage.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/analyzer"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/formatter"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/tokens"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/tracker"
)

// countTokensFn is the token-counting backend used by runStats. It is a
// package-level seam so tests can substitute a deterministic offline stub
// (success or failure) without making real Anthropic API calls.
var countTokensFn = tokens.CountTokensAPI

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	subcommand := os.Args[1]
	switch subcommand {
	case "list":
		cmdList(os.Args[2:])
	case "read":
		cmdRead(os.Args[2:])
	case "context":
		cmdContext(os.Args[2:])
	case "stats":
		cmdStats(os.Args[2:])
	case "audit":
		cmdAudit(os.Args[2:])
	case "expand":
		cmdExpand(os.Args[2:])
	case "usage":
		cmdUsage(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", subcommand)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: sessions <command> [options]")
	fmt.Fprintln(os.Stderr, "Commands: list, read, context, stats, audit, expand, usage")
}

func cmdList(args []string) {
	if err := runList(args, os.Stdout, os.Stderr, parser.DefaultStore()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runList(args []string, out io.Writer, errOut io.Writer, store parser.Store) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(errOut)
	limit := fs.Int("n", 20, "max sessions to display")
	project := fs.String("p", "", "filter by project name (case-insensitive)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit < 1 {
		return fmt.Errorf("-n must be a positive integer")
	}
	logUsageAsync("list", "")

	metaFiles, err := store.ListSessionMetaFiles()
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}

	projectFilter := ""
	if *project != "" {
		projectFilter = strings.ToLower(*project)
	}

	printed := 0
	for _, mf := range metaFiles {
		if printed >= *limit {
			break
		}

		data, err := os.ReadFile(mf.Path)
		if err != nil {
			fmt.Fprintf(errOut, "warning: skipping unreadable metadata %s: %v\n", mf.Path, err)
			continue
		}
		var meta listSessionMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			fmt.Fprintf(errOut, "warning: skipping invalid metadata %s: %v\n", mf.Path, err)
			continue
		}

		projectName := "?"
		if meta.ProjectPath != "" {
			projectName = filepath.Base(meta.ProjectPath)
		}

		if projectFilter != "" && !strings.Contains(strings.ToLower(projectName), projectFilter) {
			continue
		}

		sid := meta.SessionID
		if sid == "" {
			sid = strings.TrimSuffix(filepath.Base(mf.Path), ".json")
		}
		firstPrompt := meta.FirstPrompt
		runes := []rune(firstPrompt)
		if len(runes) > 80 {
			firstPrompt = string(runes[:77]) + "..."
		}

		dateStr := "??-??"
		if meta.StartTime != "" {
			dateStr = parser.FormatTimestamp(meta.StartTime)
		}

		fmt.Fprintf(out, "%s  %s  %-20s  %3dm  u:%d a:%d  %s\n",
			sid, dateStr, projectName, meta.DurationMinutes, meta.UserMessageCount, meta.AssistantMessageCount, firstPrompt)
		printed++
	}

	if printed == 0 {
		fmt.Fprintln(errOut, "No sessions found.")
	}
	return nil
}

type listSessionMeta struct {
	SessionID             string `json:"session_id"`
	ProjectPath           string `json:"project_path"`
	StartTime             string `json:"start_time"`
	DurationMinutes       int    `json:"duration_minutes"`
	UserMessageCount      int    `json:"user_message_count"`
	AssistantMessageCount int    `json:"assistant_message_count"`
	FirstPrompt           string `json:"first_prompt"`
}

func cmdRead(args []string) {
	if err := runRead(args, os.Stdout, os.Stderr, parser.DefaultStore()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runRead(args []string, out io.Writer, errOut io.Writer, store parser.Store) error {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	fs.SetOutput(errOut)
	maxLines := fs.Int("max-lines", 0, "max output lines (0=unlimited)")
	isVerboseAgents := fs.Bool("verbose-agents", false, "show full agent results")
	isVerboseBash := fs.Bool("verbose-bash", false, "show full Bash tool stdout/stderr")
	isVerboseThinking := fs.Bool("verbose-thinking", false, "show assistant thinking blocks")
	isVerboseCommands := fs.Bool("verbose-commands", false, "show full slash/bash command output")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}
	// 0 means unlimited (intentional); a negative cap is meaningless and was
	// previously silently treated as unlimited, hiding the user's mistake.
	if *maxLines < 0 {
		return fmt.Errorf("-max-lines must be zero (unlimited) or a positive integer")
	}

	resolved, err := resolveSession(fs, store)
	if err != nil {
		return err
	}
	logUsageAsync("read", session.ShortID(resolved.ID, 8))

	opts := formatter.FormatOptions{VerboseAgents: *isVerboseAgents, VerboseBash: *isVerboseBash, VerboseThinking: *isVerboseThinking, VerboseCommands: *isVerboseCommands}
	return formatter.FormatRead(resolved.Path, *maxLines, opts, out)
}

func cmdContext(args []string) {
	if err := runContext(args, os.Stdout, os.Stderr, parser.DefaultStore()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runContext(args []string, out io.Writer, errOut io.Writer, store parser.Store) error {
	fs := flag.NewFlagSet("context", flag.ContinueOnError)
	fs.SetOutput(errOut)
	isVerboseAgents := fs.Bool("verbose-agents", false, "show full agent results")
	isVerboseBash := fs.Bool("verbose-bash", false, "show full Bash tool stdout/stderr")
	isVerboseThinking := fs.Bool("verbose-thinking", false, "show assistant thinking blocks")
	isVerboseCommands := fs.Bool("verbose-commands", false, "show full slash/bash command output")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}

	resolved, err := resolveSession(fs, store)
	if err != nil {
		return err
	}
	logUsageAsync("context", session.ShortID(resolved.ID, 8))

	opts := formatter.FormatOptions{VerboseAgents: *isVerboseAgents, VerboseBash: *isVerboseBash, VerboseThinking: *isVerboseThinking, VerboseCommands: *isVerboseCommands}
	return formatter.FormatContextWithStore(resolved.Path, resolved.ID, opts, out, store)
}

func cmdStats(args []string) {
	if err := runStats(args, os.Stdout, os.Stderr, parser.DefaultStore()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runStats(args []string, out io.Writer, errOut io.Writer, store parser.Store) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(errOut)
	isNoTokens := fs.Bool("no-tokens", false, "skip token counting")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}

	resolved, err := resolveSession(fs, store)
	if err != nil {
		return err
	}
	logUsageAsync("stats", session.ShortID(resolved.ID, 8))

	events, err := claudecodec.ReadAll(resolved.Path)
	if err != nil {
		return fmt.Errorf("parsing transcript: %w", err)
	}

	result := analyzer.ComputeStats(events)

	shortID := session.ShortID(resolved.ID, 8)
	info, _ := os.Stat(resolved.Path)
	fileSize := float64(0)
	if info != nil {
		fileSize = float64(info.Size()) / 1024.0
	}

	fmt.Fprintf(out, "Session: %s\n", shortID)
	fmt.Fprintf(out, "Transcript: %.1fKB\n\n", fileSize)
	fmt.Fprintln(out, "=== Characters ===")
	fmt.Fprintf(out, "  Raw:      %10s\n", formatNumber(result.RawChars))
	fmt.Fprintf(out, "  Filtered: %10s\n", formatNumber(result.FilteredChars))
	if result.RawChars > 0 {
		saved := result.RawChars - result.FilteredChars
		pct := float64(saved) * 100.0 / float64(result.RawChars)
		fmt.Fprintf(out, "  Saved:    %10s (%.1f%%)\n", formatNumber(saved), pct)
	}

	fmt.Fprintln(out, "\n=== Breakdown ===")
	for _, bl := range []struct{ label, key string }{
		{"KEPT  user text:        ", "user_text"},
		{"KEPT  user answers:     ", "user_answers"},
		{"KEPT  assistant text:   ", "assistant_text"},
		{"KEPT  tool summaries:   ", "tool_summaries"},
		{"CUT   tool input (raw): ", "tool_input_raw"},
		{"CUT   tool result (raw):", "tool_result_raw"},
		{"CUT   system/noise:     ", "system_noise"},
		{"CUT   command noise:    ", "command_noise"},
	} {
		fmt.Fprintf(out, "  %s %10s\n", bl.label, formatNumber(result.Categories[bl.key]))
	}

	if *isNoTokens {
		return nil
	}

	fmt.Fprintln(out)
	// Count raw and filtered tokens concurrently: the two API calls are
	// independent, so running them in parallel roughly halves wall-clock time.
	var (
		rawAPI, filtAPI int
		errRaw, errFilt error
		wg              sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		rawAPI, errRaw = countTokensFn(result.RawText)
	}()
	go func() {
		defer wg.Done()
		filtAPI, errFilt = countTokensFn(result.FilteredText)
	}()
	wg.Wait()
	if errRaw == nil && errFilt == nil {
		saved := rawAPI - filtAPI
		fmt.Fprintln(out, "=== Tokens (Anthropic API) ===")
		fmt.Fprintf(out, "  Raw:      %10s\n", formatNumber(rawAPI))
		fmt.Fprintf(out, "  Filtered: %10s\n", formatNumber(filtAPI))
		if rawAPI > 0 {
			pct := float64(saved) * 100.0 / float64(rawAPI)
			fmt.Fprintf(out, "  Saved:    %10s (%.1f%%)\n", formatNumber(saved), pct)
		}
	} else {
		// Surface why the user is getting an estimate instead of API counts.
		// Diagnostics go to stderr so the stdout payload stays machine-clean.
		apiErr := errRaw
		if apiErr == nil {
			apiErr = errFilt
		}
		fmt.Fprintf(errOut, "warning: token API unavailable (%v), using estimate\n", apiErr)
		rawEst := tokens.EstimateTokens(result.RawText)
		filtEst := tokens.EstimateTokens(result.FilteredText)
		savedEst := rawEst - filtEst
		fmt.Fprintln(out, "=== Tokens (estimated) ===")
		fmt.Fprintf(out, "  Raw:      %10s ~\n", formatNumber(rawEst))
		fmt.Fprintf(out, "  Filtered: %10s ~\n", formatNumber(filtEst))
		if rawEst > 0 {
			pct := float64(savedEst) * 100.0 / float64(rawEst)
			fmt.Fprintf(out, "  Saved:    %10s ~ (%.1f%%)\n", formatNumber(savedEst), pct)
		}
	}
	return nil
}

func cmdAudit(args []string) {
	if err := runAudit(args, os.Stdout, os.Stderr, parser.DefaultStore()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runAudit(args []string, out io.Writer, errOut io.Writer, store parser.Store) error {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	fs.SetOutput(errOut)
	samples := fs.Int("n", 5, "number of samples per category")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}
	if *samples < 1 {
		return fmt.Errorf("-n must be a positive integer")
	}

	resolved, err := resolveSession(fs, store)
	if err != nil {
		return err
	}
	logUsageAsync("audit", session.ShortID(resolved.ID, 8))

	events, err := claudecodec.ReadAll(resolved.Path)
	if err != nil {
		return fmt.Errorf("parsing transcript: %w", err)
	}

	result := analyzer.ComputeAudit(events)

	for _, catName := range []string{"tool_result_cut", "system_noise", "thinking"} {
		items := result.Categories[catName]
		if len(items) == 0 {
			continue
		}
		shown := sampleCount(*samples, len(items))
		fmt.Fprintf(out, "=== %s (%d items, showing %d) ===\n", catName, len(items), shown)
		for _, item := range items[:shown] {
			fmt.Fprintf(out, "  %s\n\n", item)
		}
		if len(items) > shown {
			fmt.Fprintf(out, "  ... and %d more\n\n", len(items)-shown)
		}
	}
	return nil
}

func cmdExpand(args []string) {
	if err := runExpand(args, os.Stdout, os.Stderr, parser.DefaultStore()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runExpand(args []string, out io.Writer, errOut io.Writer, store parser.Store) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: sessions expand <session-id> <tool-id> [tool-id...]")
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
	logUsageAsync("expand", session.ShortID(resolved.ID, 8))

	events, err := claudecodec.ReadAll(resolved.Path)
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
		return fmt.Errorf("no matching tool IDs found. Use 'sessions read <session-id>' to see available IDs")
	}
	return nil
}


// logUsageAsync logs a usage entry in a background goroutine.
// Tracking failures are silently discarded to never affect the main command.
func logUsageAsync(cmd string, target string) {
	go func() {
		cwd, _ := os.Getwd()
		caller := tracker.DetectCallerSession(cwd)
		entry := tracker.UsageEntry{
			Timestamp: time.Now().Format(time.RFC3339),
			Command:   cmd,
			Target:    target,
			Cwd:       cwd,
			Caller:    caller,
		}
		_ = tracker.LogUsage(entry)
	}()
}

func cmdUsage(args []string) {
	if err := runUsage(args, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runUsage(args []string, out io.Writer, errOut io.Writer) error {
	fs := flag.NewFlagSet("usage", flag.ContinueOnError)
	fs.SetOutput(errOut)
	limit := fs.Int("n", 20, "max entries to display")
	cmdFilter := fs.String("cmd", "", "filter by subcommand name")
	if err := fs.Parse(args); err != nil {
		return err
	}

	entries, err := tracker.ReadUsageLog(*limit, *cmdFilter)
	if err != nil {
		return fmt.Errorf("read usage log: %w", err)
	}

	if len(entries) == 0 {
		fmt.Fprintln(errOut, "No usage entries found.")
		return nil
	}

	for _, e := range entries {
		// Parse timestamp to display in short format
		ts, parseErr := time.Parse(time.RFC3339, e.Timestamp)
		dateStr := e.Timestamp // fallback to raw
		if parseErr == nil {
			dateStr = ts.Format("2006-01-02 15:04")
		}

		target := e.Target
		if target == "" {
			target = "-"
		}

		callerShort := "-"
		if e.Caller != "" {
			callerShort = "caller:" + session.ShortID(e.Caller, 8)
		}

		fmt.Fprintf(out, "%s  %-8s %s  %s  %s\n",
			dateStr, e.Command, target, callerShort, e.Cwd)
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

// --- helpers ---

var reorderBoolFlags = map[string]bool{
	"verbose-agents":   true,
	"verbose-bash":     true,
	"verbose-thinking": true,
	"verbose-commands": true,
	"no-tokens":        true,
}

// reorderArgs moves flags before positional args so Go's flag package
// can parse them correctly. Go's flag.Parse stops at the first non-flag
// argument, but argparse (Python) allows intermixed flags and positionals.
// reorderBoolFlags must list every supported boolean flag so a following
// positional session ID is not consumed as a flag value.
func reorderArgs(args []string) []string {
	var flags []string
	var positional []string
	i := 0
	for i < len(args) {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			name := strings.TrimLeft(strings.SplitN(args[i], "=", 2)[0], "-")
			if reorderBoolFlags[name] {
				i++
				continue
			}
			// Check if next arg is the flag's value (not another flag)
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") && !strings.Contains(args[i], "=") {
				flags = append(flags, args[i+1])
				i += 2
			} else {
				i++
			}
		} else {
			positional = append(positional, args[i])
			i++
		}
	}
	return append(flags, positional...)
}

func resolveSession(fs *flag.FlagSet, store parser.Store) (parser.ResolvedSession, error) {
	if fs.NArg() < 1 {
		return parser.ResolvedSession{}, fmt.Errorf("session_id is required")
	}
	resolved, err := store.ResolveSession(fs.Arg(0))
	if err != nil {
		return parser.ResolvedSession{}, err
	}
	if resolved.Path == "" {
		return parser.ResolvedSession{}, fmt.Errorf("transcript not found: %s", resolved.ID)
	}
	return resolved, nil
}

func formatNumber(n int) string {
	// strconv.Itoa formats the sign (including math.MinInt, where -n would
	// overflow back to a negative). Group the digit portion after the sign.
	s := strconv.Itoa(n)
	sign := ""
	digits := s
	if strings.HasPrefix(s, "-") {
		sign = "-"
		digits = s[1:]
	}
	if len(digits) <= 3 {
		return s
	}
	var result []byte
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return sign + string(result)
}

func sampleCount(requested int, total int) int {
	if requested < 0 {
		return 0
	}
	if requested > total {
		return total
	}
	return requested
}
