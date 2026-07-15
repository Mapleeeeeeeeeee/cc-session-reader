package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/config"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/tracker"
)

var usageWG sync.WaitGroup

// pendingUsageCmd and pendingUsageTarget describe the in-progress command
// invocation to be recorded once its outcome is known. There is at most one
// command per process, so package-level state is safe: main's dispatch sets
// pendingUsageCmd before running a tracked command (see beginUsageTracking),
// and the command itself may later refine pendingUsageTarget once it
// resolves a concrete target (see logUsageAsync). finalizeUsageLog consumes
// and clears both when the command returns.
var (
	pendingUsageCmd    string
	pendingUsageTarget string
)

// beginUsageTracking marks cmd as the subcommand whose result should be
// recorded to usage.jsonl once it finishes. Called once from main's dispatch
// for every command with tracksUsage set, before its run executes — this way
// a command that fails before it ever resolves a target (e.g. an unknown
// session ID) still produces a usage entry, just with an empty target.
func beginUsageTracking(cmd string) {
	pendingUsageCmd = cmd
}

// logUsageAsync records target as the resolved session for the current
// tracked invocation (see beginUsageTracking). It no longer writes to disk
// itself: the actual entry is written by finalizeUsageLog once the command's
// outcome is known, so a single JSONL line always carries the correct result.
func logUsageAsync(cmd string, target string) {
	pendingUsageCmd = cmd
	pendingUsageTarget = target
}

// finalizeUsageLog writes the usage entry for the invocation that just
// finished with cmdErr (nil on success), in a background goroutine. It is a
// no-op when no command started tracking this invocation (beginUsageTracking
// was never called — e.g. "help"/"usage" itself), when CC_SESSION_NO_USAGE is
// set, or when cmdErr is flag.ErrHelp (a deliberate -h request, not a
// failure). Call waitUsageLog before process exit to ensure the write
// completes.
func finalizeUsageLog(cmdErr error) {
	if pendingUsageCmd == "" {
		return
	}
	cmd, target := pendingUsageCmd, pendingUsageTarget
	pendingUsageCmd, pendingUsageTarget = "", ""

	if config.Get().NoUsage {
		return
	}
	if errors.Is(cmdErr, flag.ErrHelp) {
		return
	}

	result := "ok"
	errMsg := ""
	if cmdErr != nil {
		result = "error"
		errMsg = firstLine(cmdErr.Error())
	}

	usageWG.Add(1)
	go func() {
		defer usageWG.Done()
		cwd, err := os.Getwd()
		if err != nil {
			return
		}
		caller := tracker.DetectCallerSession(cwd)
		if caller == "" {
			return
		}
		entry := tracker.UsageEntry{
			Timestamp: time.Now().Format(time.RFC3339),
			Command:   cmd,
			Target:    target,
			Cwd:       cwd,
			Caller:    caller,
			Version:   version,
			Commit:    commit,
			Result:    result,
			Error:     errMsg,
		}
		_ = tracker.LogUsage(entry)
	}()
}

// firstLine truncates s to its first line, so a multi-line error message
// doesn't break usage.jsonl's one-entry-per-line format.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
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
		fmt.Fprintln(out, "No usage entries found.")
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

		// Result is only present on entries recorded after the result field
		// was added; older entries render no marker rather than a
		// misleading "ok".
		resultMarker := ""
		if e.Result == "error" {
			resultMarker = " [ERR]"
		}

		fmt.Fprintf(out, "%s  %-8s %s  %s  %s%s\n",
			dateStr, e.Command, target, callerShort, e.Cwd, resultMarker)
	}
	return nil
}
