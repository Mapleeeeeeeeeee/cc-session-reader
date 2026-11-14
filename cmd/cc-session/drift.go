package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/analyzer"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/driftscan"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
)

const defaultDriftDays = 30

// dateLength is the length of the "2006-01-02" prefix of an ISO 8601 timestamp.
const dateLength = 10

func cmdDrift(args []string) {
	exitOnError(runDrift(args, os.Stdout, os.Stderr, parser.DefaultStore()))
}

func runDrift(args []string, out io.Writer, errOut io.Writer, store parser.Store) error {
	fs := flag.NewFlagSet("drift", flag.ContinueOnError)
	fs.SetOutput(errOut)
	days := fs.Int("days", defaultDriftDays, "scan transcripts modified within this many days")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}
	if *days < 1 {
		return fmt.Errorf("-days must be a positive integer")
	}

	logUsageAsync("drift", "")

	result, err := driftscan.Scan(store.ProjectsDir, time.Now().AddDate(0, 0, -*days))
	if err != nil {
		return fmt.Errorf("scanning transcripts: %w", err)
	}

	fmt.Fprintf(out, "=== Drift scan: transcripts modified in the last %d days ===\n", *days)
	fmt.Fprintf(out, "  scanned %s files (%s sessions)\n",
		analyzer.FormatNumber(result.FilesScanned), analyzer.FormatNumber(result.Sessions))
	if result.InvalidLines > 0 {
		fmt.Fprintf(out, "  skipped %s lines that were not valid JSON\n", analyzer.FormatNumber(result.InvalidLines))
	}
	if result.UnreadableFiles > 0 {
		fmt.Fprintf(out, "  could not read %s files\n", analyzer.FormatNumber(result.UnreadableFiles))
	}
	if len(result.Findings) == 0 {
		fmt.Fprintln(out, "  every shape in these transcripts is already handled")
	}

	for _, category := range driftscan.Categories {
		fmt.Fprintf(out, "\n=== Unhandled %ss ===\n", category)
		printed := false
		for _, finding := range result.Findings {
			if finding.Category != category {
				continue
			}
			printed = true
			printDriftFinding(out, finding)
		}
		if !printed {
			fmt.Fprintln(out, "  none")
		}
	}
	return nil
}

func printDriftFinding(out io.Writer, finding driftscan.Finding) {
	fmt.Fprintf(out, "  %-28s %8s records  %6s sessions  first seen in %s on %s",
		finding.Value, analyzer.FormatNumber(finding.Records), analyzer.FormatNumber(finding.Sessions),
		driftVersionLabel(finding.FirstVersion), driftDateLabel(finding.FirstTimestamp))
	if finding.NeedsAttention() {
		fmt.Fprint(out, "  ATTENTION: unknown type with message.role, may be a new conversation type")
	}
	fmt.Fprintf(out, "\n      e.g. %s:%d\n", finding.ExampleFile, finding.ExampleLine)
}

func driftVersionLabel(version string) string {
	if version == "" {
		return "unknown version"
	}
	return version
}

func driftDateLabel(timestamp string) string {
	if len(timestamp) < dateLength {
		return "unknown date"
	}
	return timestamp[:dateLength]
}
