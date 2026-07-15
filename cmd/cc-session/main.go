// Package main is the CLI entry point for the Claude session reader.
// Run "cc-session help" for a usage cheat sheet, or see the command
// registry in commands.go for the authoritative subcommand list.
package main

import (
	"fmt"
	"os"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/tokens"
)

var version = "dev"
var commit = "none"

type countTokensFunc func(string) (int, error)

// countTokensFn is the token-counting backend used by runStats. It is a
// package-level seam so tests can substitute a deterministic offline stub
// (success or failure) without making real Anthropic API calls.
var countTokensFn countTokensFunc = tokens.CountTokensAPI

// newCountTokensFn builds a reusable token-counting backend for commands that
// count multiple inputs in one run.
var newCountTokensFn = func(model string) (countTokensFunc, error) {
	counter, err := tokens.NewCounter(model)
	if err != nil {
		return nil, err
	}
	return counter.Count, nil
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	defer waitUsageLog()

	reader := claudecodec.Codec{}

	subcommand := os.Args[1]
	switch subcommand {
	case "-h", "--help":
		printUsage()
		return
	case "-v", "--version", "version":
		fmt.Printf("cc-session %s\n", version)
		return
	default:
		cmd, ok := findCommand(subcommand)
		if !ok {
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n", subcommand)
			printUsage()
			os.Exit(1)
		}
		cmd.run(os.Args[2:], reader)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: cc-session <command> [options]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	nameFormat := fmt.Sprintf("  %%-%ds  %%s\n", longestVisibleCommandNameLen())
	for _, cmd := range commands {
		if cmd.hidden {
			continue
		}
		fmt.Fprintf(os.Stderr, nameFormat, cmd.name, cmd.summary)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Run 'cc-session <command> -h' for command-specific flags.")
}

// longestVisibleCommandNameLen returns the length of the longest non-hidden
// command name, so printUsage can pad its name column wide enough for every
// entry (a fixed width goes ragged once a name like "benchmark" exceeds it).
func longestVisibleCommandNameLen() int {
	maxLen := 0
	for _, cmd := range commands {
		if cmd.hidden {
			continue
		}
		if len(cmd.name) > maxLen {
			maxLen = len(cmd.name)
		}
	}
	return maxLen
}
