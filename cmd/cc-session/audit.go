package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/analyzer"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func cmdAudit(args []string, reader session.TranscriptReader) {
	exitOnError(runAudit(args, os.Stdout, os.Stderr, parser.DefaultStore(), reader))
}

func runAudit(args []string, out io.Writer, errOut io.Writer, store parser.Store, reader session.TranscriptReader) error {
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

	events, err := reader.ReadAll(resolved.Path)
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
