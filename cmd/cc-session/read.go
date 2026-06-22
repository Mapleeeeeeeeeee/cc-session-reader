package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/formatter"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func cmdRead(args []string, reader session.TranscriptReader) {
	exitOnError(runRead(args, os.Stdout, os.Stderr, parser.DefaultStore(), reader))
}

func runRead(args []string, out io.Writer, errOut io.Writer, store parser.Store, reader session.TranscriptReader) error {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	fs.SetOutput(errOut)
	maxLines := fs.Int("max-lines", 200, "max output lines (0=unlimited)")
	offset := fs.Int("offset", 0, "skip first N output lines")
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
	if *offset < 0 {
		return fmt.Errorf("-offset must be zero or a positive integer")
	}

	resolved, err := resolveSession(fs, store)
	if err != nil {
		return err
	}
	logUsageAsync("read", session.ShortID(resolved.ID, 8))

	opts := formatter.FormatOptions{VerboseAgents: *isVerboseAgents, VerboseBash: *isVerboseBash, VerboseThinking: *isVerboseThinking, VerboseCommands: *isVerboseCommands}
	return formatter.FormatRead(resolved.Path, *maxLines, *offset, opts, out, reader)
}
