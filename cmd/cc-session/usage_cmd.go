package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/tracker"
)

var usageWG sync.WaitGroup

// logUsageAsync logs a usage entry in a background goroutine.
// Call waitUsageLog before process exit to ensure the write completes.
func logUsageAsync(cmd string, target string) {
	usageWG.Add(1)
	go func() {
		defer usageWG.Done()
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

func waitUsageLog() { usageWG.Wait() }

func cmdUsage(args []string) {
	exitOnError(runUsage(args, os.Stdout, os.Stderr))
}

func runUsage(args []string, out io.Writer, errOut io.Writer) error {
	fs := flag.NewFlagSet("usage", flag.ContinueOnError)
	fs.SetOutput(errOut)
	limit := fs.Int("n", 20, "max entries to display")
	cmdFilter := fs.String("cmd", "", "filter by subcommand name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit < 1 {
		return fmt.Errorf("-n must be >= 1, got %d", *limit)
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
