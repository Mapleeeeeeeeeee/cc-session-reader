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

	fmt.Fprintln(out, "=== CUT-content histogram (risk-descending) ===")
	for _, bucket := range analyzer.BucketOrder {
		items := result.Samples[bucket]
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(out, "  %-24s %6s items  %12s chars\n",
			bucket, analyzer.FormatNumber(len(items)), analyzer.FormatNumber(result.BucketChars[bucket]))
	}

	fmt.Fprintln(out, "\n=== Failed tool result length (chars) ===")
	if result.Failures.Count == 0 {
		fmt.Fprintln(out, "  no failed tool results in this session")
	} else {
		f := result.Failures
		fmt.Fprintf(out, "  count: %s   median: %s   p90: %s   max: %s\n",
			analyzer.FormatNumber(f.Count), analyzer.FormatNumber(f.Median),
			analyzer.FormatNumber(f.P90), analyzer.FormatNumber(f.Max))
	}

	fmt.Fprintln(out, "\n=== Samples ===")
	for _, bucket := range analyzer.BucketOrder {
		items := result.Samples[bucket]
		if len(items) == 0 {
			continue
		}
		shown := sampleCount(*samples, len(items))
		fmt.Fprintf(out, "=== %s (%d items, showing %d) ===\n", bucket, len(items), shown)
		for _, item := range items[:shown] {
			fmt.Fprintf(out, "  %s\n\n", item)
		}
		if len(items) > shown {
			fmt.Fprintf(out, "  ... and %d more\n\n", len(items)-shown)
		}
	}
	return nil
}
