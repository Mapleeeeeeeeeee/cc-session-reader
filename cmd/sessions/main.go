// Package main is the CLI entry point for the Claude session reader.
// Subcommands: list, read, context, stats, audit.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/analyzer"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/formatter"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/tokens"
)

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
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", subcommand)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: sessions <command> [options]")
	fmt.Fprintln(os.Stderr, "Commands: list, read, context, stats, audit")
}

func cmdList(args []string) {
	if err := runList(args, os.Stdout, os.Stderr, parser.DefaultStore()); err != nil {
		fmt.Fprintln(os.Stderr, err)
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
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}

	resolved, err := resolveSession(fs, store)
	if err != nil {
		return err
	}

	return formatter.FormatRead(resolved.Path, *maxLines, *isVerboseAgents, out)
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
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}

	resolved, err := resolveSession(fs, store)
	if err != nil {
		return err
	}

	return formatter.FormatContextWithStore(resolved.Path, resolved.ID, *isVerboseAgents, out, store)
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
	} {
		fmt.Fprintf(out, "  %s %10s\n", bl.label, formatNumber(result.Categories[bl.key]))
	}

	if *isNoTokens {
		return nil
	}

	fmt.Fprintln(out)
	rawAPI, errRaw := tokens.CountTokensAPI(result.RawText)
	filtAPI, errFilt := tokens.CountTokensAPI(result.FilteredText)
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
		fmt.Fprintln(os.Stderr, err)
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

	resolved, err := resolveSession(fs, store)
	if err != nil {
		return err
	}

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

// --- helpers ---

var reorderBoolFlags = map[string]bool{
	"-verbose-agents":  true,
	"--verbose-agents": true,
	"-no-tokens":       true,
	"--no-tokens":      true,
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
			name := strings.SplitN(args[i], "=", 2)[0]
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
	if n < 0 {
		return "-" + formatNumber(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
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
