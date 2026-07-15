// Package main is the CLI entry point for the Claude session reader.
// Run "cc-session help" for a usage cheat sheet, or see the command
// registry in commands.go for the authoritative subcommand list.
package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/tokens"
)

var version = "dev"
var commit = "none"

// readBuildInfo is the debug.ReadBuildInfo backend used by resolveVersion. It
// is a package-level seam so tests can substitute deterministic build info
// instead of whatever the go test binary itself was built with.
var readBuildInfo = debug.ReadBuildInfo

// resolveVersion returns version unchanged unless it's still the "dev"
// placeholder, meaning goreleaser's ldflags -X override (see .goreleaser.yaml)
// never ran — a plain `go build`/`go install` of this module. In that case it
// falls back to runtime/debug build info: `go install pkg@version` embeds the
// module version in info.Main.Version, while a source build embeds the VCS
// revision in info.Settings. Returns "dev" unchanged if neither is available.
func resolveVersion(version string) string {
	if version != "dev" {
		return version
	}
	info, ok := readBuildInfo()
	if !ok {
		return version
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			rev := s.Value
			if len(rev) > 12 {
				rev = rev[:12]
			}
			return "dev+" + rev
		}
	}
	return version
}

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

	version = resolveVersion(version)

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
		if cmd.tracksUsage {
			beginUsageTracking(cmd.name)
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
