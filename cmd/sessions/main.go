// Package main is the CLI entry point for the Claude session reader.
// Subcommands: list, read, context, stats, audit.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claude-code-session-reader/internal/analyzer"
	"claude-code-session-reader/internal/formatter"
	"claude-code-session-reader/internal/jsonutil"
	"claude-code-session-reader/internal/parser"
	"claude-code-session-reader/internal/tokens"
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
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	limit := fs.Int("n", 20, "max sessions to display")
	project := fs.String("p", "", "filter by project name (case-insensitive)")
	_ = fs.Parse(args)

	metaFiles, err := parser.ListSessionMetaFiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing sessions: %v\n", err)
		os.Exit(1)
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
			continue
		}
		var meta map[string]interface{}
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}

		projectPath := jsonutil.GetStr(meta, "project_path")
		projectName := "?"
		if projectPath != "" {
			projectName = filepath.Base(projectPath)
		}

		if projectFilter != "" && !strings.Contains(strings.ToLower(projectName), projectFilter) {
			continue
		}

		sid := jsonutil.GetStr(meta, "session_id")
		if sid == "" {
			sid = strings.TrimSuffix(filepath.Base(mf.Path), ".json")
		}
		startTime := jsonutil.GetStr(meta, "start_time")
		duration := jsonutil.GetNum(meta, "duration_minutes")
		userMsgs := jsonutil.GetNum(meta, "user_message_count")
		asstMsgs := jsonutil.GetNum(meta, "assistant_message_count")
		firstPrompt := jsonutil.GetStr(meta, "first_prompt")
		runes := []rune(firstPrompt)
		if len(runes) > 80 {
			firstPrompt = string(runes[:77]) + "..."
		}

		dateStr := "??-??"
		if startTime != "" {
			dateStr = parser.FormatTimestamp(startTime)
		}

		fmt.Printf("%s  %s  %-20s  %3dm  u:%d a:%d  %s\n",
			sid, dateStr, projectName, duration, userMsgs, asstMsgs, firstPrompt)
		printed++
	}

	if printed == 0 {
		fmt.Fprintln(os.Stderr, "No sessions found.")
	}
}

func cmdRead(args []string) {
	fs := flag.NewFlagSet("read", flag.ExitOnError)
	maxLines := fs.Int("max-lines", 0, "max output lines (0=unlimited)")
	isVerboseAgents := fs.Bool("verbose-agents", false, "show full agent results")
	_ = fs.Parse(reorderArgs(args))

	sessionID := resolveSessionArg(fs)
	transcriptPath := findTranscriptOrExit(sessionID)

	if err := formatter.FormatRead(transcriptPath, sessionID, *maxLines, *isVerboseAgents, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdContext(args []string) {
	fs := flag.NewFlagSet("context", flag.ExitOnError)
	isVerboseAgents := fs.Bool("verbose-agents", false, "show full agent results")
	_ = fs.Parse(reorderArgs(args))

	sessionID := resolveSessionArg(fs)
	transcriptPath := findTranscriptOrExit(sessionID)

	if err := formatter.FormatContext(transcriptPath, sessionID, *isVerboseAgents, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdStats(args []string) {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	isNoTokens := fs.Bool("no-tokens", false, "skip token counting")
	_ = fs.Parse(reorderArgs(args))

	sessionID := resolveSessionArg(fs)
	transcriptPath := findTranscriptOrExit(sessionID)

	entries, err := parser.ParseTranscript(transcriptPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing transcript: %v\n", err)
		os.Exit(1)
	}

	result := analyzer.ComputeStats(entries)

	shortID := sessionID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	info, _ := os.Stat(transcriptPath)
	fileSize := float64(0)
	if info != nil {
		fileSize = float64(info.Size()) / 1024.0
	}

	fmt.Printf("Session: %s\n", shortID)
	fmt.Printf("Transcript: %.1fKB\n\n", fileSize)
	fmt.Println("=== Characters ===")
	fmt.Printf("  Raw:      %10s\n", formatNumber(result.RawChars))
	fmt.Printf("  Filtered: %10s\n", formatNumber(result.FilteredChars))
	if result.RawChars > 0 {
		saved := result.RawChars - result.FilteredChars
		pct := float64(saved) * 100.0 / float64(result.RawChars)
		fmt.Printf("  Saved:    %10s (%.1f%%)\n", formatNumber(saved), pct)
	}

	fmt.Println("\n=== Breakdown ===")
	for _, bl := range []struct{ label, key string }{
		{"KEPT  user text:        ", "user_text"},
		{"KEPT  user answers:     ", "user_answers"},
		{"KEPT  assistant text:   ", "assistant_text"},
		{"KEPT  tool summaries:   ", "tool_summaries"},
		{"CUT   tool input (raw): ", "tool_input_raw"},
		{"CUT   tool result (raw):", "tool_result_raw"},
		{"CUT   system/noise:     ", "system_noise"},
	} {
		fmt.Printf("  %s %10s\n", bl.label, formatNumber(result.Categories[bl.key]))
	}

	if *isNoTokens {
		return
	}

	fmt.Println()
	rawAPI, errRaw := tokens.CountTokensAPI(result.RawText)
	filtAPI, errFilt := tokens.CountTokensAPI(result.FilteredText)
	if errRaw == nil && errFilt == nil {
		saved := rawAPI - filtAPI
		fmt.Println("=== Tokens (Anthropic API) ===")
		fmt.Printf("  Raw:      %10s\n", formatNumber(rawAPI))
		fmt.Printf("  Filtered: %10s\n", formatNumber(filtAPI))
		if rawAPI > 0 {
			pct := float64(saved) * 100.0 / float64(rawAPI)
			fmt.Printf("  Saved:    %10s (%.1f%%)\n", formatNumber(saved), pct)
		}
	} else {
		rawEst := tokens.EstimateTokens(result.RawText)
		filtEst := tokens.EstimateTokens(result.FilteredText)
		savedEst := rawEst - filtEst
		fmt.Println("=== Tokens (estimated) ===")
		fmt.Printf("  Raw:      %10s ~\n", formatNumber(rawEst))
		fmt.Printf("  Filtered: %10s ~\n", formatNumber(filtEst))
		if rawEst > 0 {
			pct := float64(savedEst) * 100.0 / float64(rawEst)
			fmt.Printf("  Saved:    %10s ~ (%.1f%%)\n", formatNumber(savedEst), pct)
		}
	}
}

func cmdAudit(args []string) {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	samples := fs.Int("n", 5, "number of samples per category")
	_ = fs.Parse(reorderArgs(args))

	sessionID := resolveSessionArg(fs)
	transcriptPath := findTranscriptOrExit(sessionID)

	entries, err := parser.ParseTranscript(transcriptPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing transcript: %v\n", err)
		os.Exit(1)
	}

	result := analyzer.ComputeAudit(entries)

	for _, catName := range []string{"tool_result_cut", "system_noise", "thinking"} {
		items := result.Categories[catName]
		if len(items) == 0 {
			continue
		}
		shown := sampleCount(*samples, len(items))
		fmt.Printf("=== %s (%d items, showing %d) ===\n", catName, len(items), shown)
		for _, item := range items[:shown] {
			fmt.Printf("  %s\n\n", item)
		}
		if len(items) > shown {
			fmt.Printf("  ... and %d more\n\n", len(items)-shown)
		}
	}
}

// --- helpers ---

// reorderArgs moves flags before positional args so Go's flag package
// can parse them correctly. Go's flag.Parse stops at the first non-flag
// argument, but argparse (Python) allows intermixed flags and positionals.
// This makes `audit 3537152c -n 2` work the same as `audit -n 2 3537152c`.
func reorderArgs(args []string) []string {
	var flags []string
	var positional []string
	i := 0
	for i < len(args) {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
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

func resolveSessionArg(fs *flag.FlagSet) string {
	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Error: session_id is required\n")
		os.Exit(1)
	}
	sessionID, err := parser.ResolveSessionID(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	return sessionID
}

func findTranscriptOrExit(sessionID string) string {
	path := parser.FindTranscript(sessionID)
	if path == "" {
		fmt.Fprintf(os.Stderr, "Transcript not found: %s\n", sessionID)
		os.Exit(1)
	}
	return path
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
