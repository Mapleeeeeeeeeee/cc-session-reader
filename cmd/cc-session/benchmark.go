package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/analyzer"
	bm "github.com/Mapleeeeeeeeeee/cc-session-reader/internal/benchmark"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func cmdBenchmark(args []string, reader session.TranscriptReader) {
	store := parser.DefaultStore()
	if hs, ok := reader.(session.HeaderScanner); ok {
		store = parser.DefaultStoreWith(hs)
	}
	exitOnError(runBenchmark(args, os.Stdout, os.Stderr, store, reader))
}

func runBenchmark(args []string, out io.Writer, errOut io.Writer, store parser.Store, reader session.TranscriptReader) error {
	fs := flag.NewFlagSet("benchmark", flag.ContinueOnError)
	fs.SetOutput(errOut)
	days := fs.Int("days", 30, "how far back to scan")
	minKB := fs.Int("min-kb", 100, "minimum JSONL file size in KB")
	maxN := fs.Int("n", 10, "max sessions to include")
	model := fs.String("model", "opus", "model: opus, opus-4-6, opus-4-7, opus-4-8, or sonnet")
	overhead := fs.Int("overhead", 0, "session overhead tokens (system+tools+CLAUDE.md); measure with a 1-turn session")
	isNoAPI := fs.Bool("no-api", false, "skip API calls; estimate filtered-text tokens with chars/2 (offline fallback)")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}

	logUsageAsync("benchmark", "")

	overheadToks := *overhead
	if overheadToks <= 0 {
		overheadToks = bm.DefaultOverhead
	}

	modelConfig, err := bm.ResolveModel(*model)
	if err != nil {
		return err
	}
	p := modelConfig.Pricing
	tokenCountModel := modelConfig.TokenCountModel

	all, _ := store.ListAllSessions()

	cutoff := time.Now().AddDate(0, 0, -*days)

	type candidate struct {
		entry    parser.SessionListEntry
		fileSize int64
		path     string
	}

	var candidates []candidate
	for _, entry := range all {
		if !entry.StartTimeParsed.IsZero() && entry.StartTimeParsed.Before(cutoff) {
			continue
		}
		resolved, err := store.ResolveSession(entry.SessionID)
		if err != nil || resolved.Path == "" {
			continue
		}
		info, err := os.Stat(resolved.Path)
		if err != nil {
			continue
		}
		sizeKB := info.Size() / 1024
		if sizeKB < int64(*minKB) {
			continue
		}
		candidates = append(candidates, candidate{entry: entry, fileSize: info.Size(), path: resolved.Path})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].fileSize > candidates[j].fileSize
	})

	if len(candidates) == 0 {
		fmt.Fprintln(out, "No sessions matched the filters.")
		return nil
	}

	var results []bm.Result
	var tokenCounter countTokensFunc
	for _, c := range candidates {
		if len(results) >= *maxN {
			break
		}
		events, err := reader.ReadAll(c.path)
		if err != nil {
			continue
		}
		stats := analyzer.ComputeStats(events)

		contextToks := stats.LastContextTokens
		if contextToks == 0 {
			fmt.Fprintf(out, "  skipping %s: missing API usage data\n",
				session.ShortID(c.entry.SessionID, 8))
			continue
		}

		if stats.CompactCount > 0 {
			fmt.Fprintf(out, "  skipping %s: compacted (%d times)\n",
				session.ShortID(c.entry.SessionID, 8), stats.CompactCount)
			continue
		}

		var filteredToks int
		if *isNoAPI {
			filteredToks = stats.FilteredChars / bm.CharsPerToken
		} else {
			if tokenCounter == nil {
				tokenCounter, err = newCountTokensFn(tokenCountModel)
				if err != nil {
					return fmt.Errorf("initialize token counter: %w", err)
				}
			}
			filteredToks, err = tokenCounter(stats.FilteredText)
			if err != nil {
				return fmt.Errorf("count filtered tokens for %s: %w", session.ShortID(c.entry.SessionID, 8), err)
			}
		}
		newContextToks := overheadToks + filteredToks

		savedPct := float64(contextToks-newContextToks) * 100.0 / float64(contextToks)

		cpt := 1.0
		if stats.UserTurnCount > 0 && stats.APICallCount > stats.UserTurnCount {
			cpt = float64(stats.APICallCount) / float64(stats.UserTurnCount)
		}

		// Derive perCallToolIO from actual PerTool data (chars → tokens).
		// PerTool.InputChars = tool_use JSON, PerTool.ResultChars = tool_result text.
		// Empirically measured weighted average: ~1.86 chars/token; chars/2 is the best
		// estimate without sending raw tool text to the API.
		toolIO := bm.PerCallToolIO // fallback to constant
		totalToolChars := 0
		totalToolCalls := 0
		for _, ts := range stats.PerTool {
			totalToolChars += ts.InputChars + ts.ResultChars
			totalToolCalls += ts.CallCount
		}
		if totalToolCalls > 0 {
			toolIO = totalToolChars / totalToolCalls / bm.CharsPerToken
			if toolIO < 500 {
				toolIO = 500
			}
		}

		// Derive avg response tokens from actual output data.
		avgResp := bm.PerTurnResponse // fallback
		if stats.APICallCount > 0 {
			avgResp = stats.TotalOutputTokens / stats.APICallCount
		}

		// Derive perTurnPrompt from context growth:
		//   total_growth = LastContextTokens - overhead
		//   growth_per_turn = total_growth / UserTurnCount
		//   perTurnPrompt ≈ growth_per_turn - avgResponse - toolIO*(K-1)
		prompt := bm.PerTurnPrompt // fallback
		if stats.UserTurnCount > 0 && contextToks > overheadToks {
			growthPerTurn := (contextToks - overheadToks) / stats.UserTurnCount
			toolIOPerTurn := int(float64(toolIO) * (cpt - 1))
			derived := growthPerTurn - avgResp - toolIOPerTurn
			if derived > 0 {
				prompt = derived
			}
		}

		br := bm.Result{
			ShortID:          session.ShortID(c.entry.SessionID, 8),
			ContextTokens:    contextToks,
			FilteredTokens:   filteredToks,
			NewContextTokens: newContextToks,
			SavedPct:         savedPct,
			CallsPerTurn:     cpt,
			ToolIOPerCall:    toolIO,
			AvgResponse:      avgResp,
			Prompt:           prompt,
		}
		bm.ComputeCostMetrics(&br, overheadToks, p)
		results = append(results, br)
	}

	fmt.Fprintln(out)
	if len(results) == 0 {
		fmt.Fprintln(out, "No sessions could be processed.")
		return nil
	}

	bm.PrintCompressionSection(out, results)
	bm.PrintCostSummary(out, results, p, *model)
	fmt.Fprintln(out)
	bm.PrintWarmCostSummary(out, results, p, *model)

	return nil
}
