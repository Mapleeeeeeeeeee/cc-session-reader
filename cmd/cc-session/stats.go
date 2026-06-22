package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/analyzer"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/tokens"
)

func cmdStats(args []string, reader session.TranscriptReader) {
	exitOnError(runStats(args, os.Stdout, os.Stderr, parser.DefaultStore(), reader))
}

func runStats(args []string, out io.Writer, errOut io.Writer, store parser.Store, reader session.TranscriptReader) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(errOut)
	isNoTokens := fs.Bool("no-tokens", false, "skip token counting")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}

	resolved, err := resolveSession(fs, store)
	if err != nil {
		return err
	}
	logUsageAsync("stats", session.ShortID(resolved.ID, 8))

	events, err := reader.ReadAll(resolved.Path)
	if err != nil {
		return fmt.Errorf("parsing transcript: %w", err)
	}

	result := analyzer.ComputeStats(events)

	info, _ := os.Stat(resolved.Path)
	fileSize := float64(0)
	if info != nil {
		fileSize = float64(info.Size()) / 1024.0
	}

	opts := analyzer.RenderOptions{
		SessionID:    session.ShortID(resolved.ID, 8),
		TranscriptKB: fileSize,
		SkipTokens:   *isNoTokens,
		HasAPIData:   result.APICallCount > 0,
	}

	if !*isNoTokens {
		if result.APICallCount > 0 {
			filtAPI, errFilt := countTokensFn(result.FilteredText)
			opts.FilteredTokens = filtAPI
			opts.TokenErr = errFilt
		} else {
			var (
				rawAPI, filtAPI int
				errRaw, errFilt error
				wg              sync.WaitGroup
			)
			wg.Add(2)
			go func() {
				defer wg.Done()
				rawAPI, errRaw = countTokensFn(result.RawText)
			}()
			go func() {
				defer wg.Done()
				filtAPI, errFilt = countTokensFn(result.FilteredText)
			}()
			wg.Wait()
			if errRaw == nil && errFilt == nil {
				opts.RawTokens = rawAPI
				opts.FilteredTokens = filtAPI
			} else {
				opts.RawTokens = tokens.EstimateTokens(result.RawText)
				opts.FilteredTokens = tokens.EstimateTokens(result.FilteredText)
				opts.TokenErr = errFilt
				opts.UseEstimate = true
			}
		}
	}

	analyzer.RenderStats(out, errOut, result, opts)
	return nil
}
