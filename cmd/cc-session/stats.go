package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/analyzer"
	bm "github.com/Mapleeeeeeeeeee/cc-session-reader/internal/benchmark"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func cmdStats(args []string, reader session.TranscriptReader) {
	exitOnError(runStats(args, os.Stdout, os.Stderr, parser.DefaultStore(), reader))
}

func runStats(args []string, out io.Writer, errOut io.Writer, store parser.Store, reader session.TranscriptReader) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(errOut)
	isNoTokens := fs.Bool("no-tokens", false, "skip token counting")
	overhead := fs.Int("overhead", 0, bm.OverheadFlagUsage)
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
		countTokens, tokenCounterErr := newCountTokensFn("")
		if result.APICallCount > 0 {
			if tokenCounterErr != nil {
				opts.TokenErr = tokenCounterErr
			} else {
				filtAPI, errFilt := countTokens(result.FilteredText)
				opts.FilteredTokens = filtAPI
				opts.OverheadTokens = bm.ResolveOverhead(*overhead)
				opts.NewContextTokens = bm.NewContextTokens(opts.OverheadTokens, filtAPI)
				opts.TokenErr = errFilt
			}
		} else {
			var (
				rawAPI, filtAPI int
				errRaw, errFilt error
				wg              sync.WaitGroup
			)
			if tokenCounterErr != nil {
				opts.TokenErr = tokenCounterErr
			} else {
				wg.Add(2)
				go func() {
					defer wg.Done()
					rawAPI, errRaw = countTokens(result.RawText)
				}()
				go func() {
					defer wg.Done()
					filtAPI, errFilt = countTokens(result.FilteredText)
				}()
				wg.Wait()
				if errRaw == nil && errFilt == nil {
					opts.RawTokens = rawAPI
					opts.FilteredTokens = filtAPI
				} else {
					if errFilt != nil {
						opts.TokenErr = errFilt
					} else {
						opts.TokenErr = errRaw
					}
				}
			}
		}
	}

	analyzer.RenderStats(out, errOut, result, opts)
	return nil
}
