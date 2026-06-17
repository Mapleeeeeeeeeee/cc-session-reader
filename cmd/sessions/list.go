package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
)

func cmdList(args []string) {
	if err := runList(args, os.Stdout, os.Stderr, parser.DefaultStoreWith(claudecodec.Codec{})); err != nil {
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

	projectFilter := ""
	if *project != "" {
		projectFilter = strings.ToLower(*project)
	}

	entries, warnings := store.ListAllSessions()
	for _, w := range warnings {
		fmt.Fprintln(errOut, w)
	}

	printed := 0
	for _, entry := range entries {
		if printed >= *limit {
			break
		}

		projectName := "?"
		if entry.ProjectPath != "" {
			projectName = filepath.Base(entry.ProjectPath)
		}

		if projectFilter != "" && !strings.Contains(strings.ToLower(projectName), projectFilter) {
			continue
		}

		dateStr := "??-??"
		if entry.StartTime != "" {
			dateStr = parser.FormatTimestamp(entry.StartTime)
		}

		fmt.Fprintf(out, "%s  %s  %-20s  %3dm  u:%d a:%d  %s\n",
			entry.SessionID, dateStr, projectName, entry.DurationMinutes, entry.UserMessageCount, entry.AssistantMessageCount, entry.FirstPrompt)
		printed++
	}

	if printed == 0 {
		fmt.Fprintln(errOut, "No sessions found.")
	}
	return nil
}
