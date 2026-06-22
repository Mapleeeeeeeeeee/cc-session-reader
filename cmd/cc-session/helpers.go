package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
)

func exitOnError(err error) {
	if err == nil {
		return
	}
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

var reorderBoolFlags = map[string]bool{
	"verbose-agents":   true,
	"verbose-bash":     true,
	"verbose-thinking": true,
	"verbose-commands": true,
	"no-tokens":        true,
	"reset":            true,
}

// reorderArgs moves flags before positional args so Go's flag package
// can parse them correctly. Go's flag.Parse stops at the first non-flag
// argument, but argparse (Python) allows intermixed flags and positionals.
// reorderBoolFlags must list every supported boolean flag so a following
// positional session ID is not consumed as a flag value.
func reorderArgs(args []string) []string {
	var flags []string
	var positional []string
	i := 0
	for i < len(args) {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			name := strings.TrimLeft(strings.SplitN(args[i], "=", 2)[0], "-")
			if reorderBoolFlags[name] {
				i++
				continue
			}
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

func resolveSession(fs *flag.FlagSet, store parser.Store) (parser.ResolvedSession, error) {
	if fs.NArg() < 1 {
		return parser.ResolvedSession{}, fmt.Errorf("session_id is required")
	}
	resolved, err := store.ResolveSession(fs.Arg(0))
	if err != nil {
		return parser.ResolvedSession{}, err
	}
	if resolved.Path == "" {
		return parser.ResolvedSession{}, fmt.Errorf("transcript not found: %s", resolved.ID)
	}
	return resolved, nil
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
