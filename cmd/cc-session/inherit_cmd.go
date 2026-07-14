package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/inject"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func cmdInherit(args []string, reader session.TranscriptReader) {
	exitOnError(runInherit(args, os.Stdout, os.Stderr, parser.DefaultStore(), reader))
}

func runInherit(args []string, out io.Writer, errOut io.Writer, store parser.Store, reader session.TranscriptReader) error {
	fs := flag.NewFlagSet("inherit", flag.ContinueOnError)
	fs.SetOutput(errOut)
	pageFlag := fs.Int("page", 0, "jump to specific page (1-based; 0 = auto-advance)")
	resetFlag := fs.Bool("reset", false, "clear state and start from the beginning")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: cc-session inherit <session-id> [--page N] [--reset]")
	}

	sessionPrefix := fs.Arg(0)
	resolved, err := store.ResolveSession(sessionPrefix)
	if err != nil {
		return err
	}
	if resolved.Path == "" {
		return fmt.Errorf("transcript not found: %s", resolved.ID)
	}
	logUsageAsync("inherit", session.ShortID(resolved.ID, 8))

	if *resetFlag {
		if err := inject.ClearState(resolved.ID); err != nil {
			return fmt.Errorf("reset state: %w", err)
		}
		return nil
	}

	fullText, err := inject.RenderFullOutput(resolved.Path, reader)
	if err != nil {
		return fmt.Errorf("render session: %w", err)
	}

	allLines := strings.Split(fullText, "\n")
	// strings.Split on a trailing newline yields an empty final element; drop it.
	if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
		allLines = allLines[:len(allLines)-1]
	}

	pages := inject.SplitPages(allLines)
	totalPages := len(pages)
	if totalPages == 0 {
		fmt.Fprintln(out, "[inherit complete: 0 pages, 0 lines]")
		return nil
	}

	totalLines := len(allLines)

	// Determine which page to display.
	var pageNum int
	if *pageFlag > 0 {
		pageNum = *pageFlag
	} else {
		state, loadErr := inject.LoadState(resolved.ID)
		if loadErr != nil {
			return fmt.Errorf("load inherit state: %w", loadErr)
		}
		if state == nil {
			pageNum = 1
		} else {
			pageNum = state.Page + 1
		}
	}

	if pageNum > totalPages {
		fmt.Fprintf(out, "[inherit complete: %d pages, %d lines] — use -reset to start over\n", totalPages, totalLines)
		return nil
	}
	if pageNum < 1 {
		pageNum = 1
	}

	// Calculate start line for the chosen page.
	startLine := 0
	for i := 0; i < pageNum-1; i++ {
		startLine += len(pages[i])
	}

	pageLines := pages[pageNum-1]

	// Add the header/footer overhead chars for limit check.
	// The overhead is small (<200 chars) and stays well within the 20K limit.
	inject.WritePage(pageLines, pageNum, totalPages, startLine, totalLines, out)

	// Persist state so next call auto-advances.
	newState := inject.State{
		SessionID:  resolved.ID,
		OffsetLine: startLine + len(pageLines),
		TotalLines: totalLines,
		Page:       pageNum,
	}
	if err := inject.SaveState(newState); err != nil {
		// Non-fatal: output was already written.
		fmt.Fprintf(errOut, "warning: could not save inherit state: %v\n", err)
	}
	return nil
}
