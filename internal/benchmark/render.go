package benchmark

import (
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/analyzer"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/models"
)

// PrintCompressionSection writes the compression comparison table to out.
func PrintCompressionSection(out io.Writer, results []Result) {
	if len(results) == 0 {
		return
	}
	fmt.Fprintln(out, "=== Compression ===")
	fmt.Fprintf(out, "%-10s  %10s  %10s  %6s  %6s\n", "Session", "Context", "NewCtx", "Ratio", "Saved")
	for _, r := range results {
		ratio := r.RatioPct()
		fmt.Fprintf(out, "%-10s  %10s  %10s  %5.1f%%  %.1f%%\n",
			r.ShortID,
			analyzer.FormatNumber(r.ContextTokens),
			analyzer.FormatNumber(r.NewContextTokens),
			ratio,
			r.SavedPct,
		)
	}
	fmt.Fprintln(out)

	pcts := make([]float64, len(results))
	for i, r := range results {
		pcts[i] = r.SavedPct
	}
	sort.Float64s(pcts)
	mean := 0.0
	for _, p := range pcts {
		mean += p
	}
	mean /= float64(len(pcts))

	fmt.Fprintf(out, "Median: %.1f%%   Mean: %.1f%%   Range: %.1f%% — %.1f%%\n\n",
		MedianFloat64(pcts), mean, pcts[0], pcts[len(pcts)-1])
}

// PrintCostSummary writes the cold-cache cost comparison table to out.
func PrintCostSummary(out io.Writer, results []Result, p models.Pricing, modelName string) {
	if len(results) == 0 {
		return
	}
	printCostSection(out, results, modelName,
		func(r Result) (int, float64, float64) { return r.BreakEven, r.Saving10Pct, r.Saving100Pct },
		"=== Cost Savings Per Session (%s) ===\n",
	)
}

// PrintWarmCostSummary writes the warm-cache cost comparison table to out.
func PrintWarmCostSummary(out io.Writer, results []Result, p models.Pricing, modelName string) {
	if len(results) == 0 {
		return
	}
	printCostSection(out, results, modelName,
		func(r Result) (int, float64, float64) { return r.WarmBreakEven, r.WarmSaving10Pct, r.WarmSaving100Pct },
		"=== Warm Cache: Cost Savings Per Session (%s) ===\n",
	)
}

func printCostSection(out io.Writer, results []Result, modelName string, fields func(Result) (int, float64, float64), titleFmt string) {
	fmt.Fprintf(out, titleFmt, modelName)
	fmt.Fprintf(out, "%-10s  %10s  %10s  %6s  %5s  %10s  %8s  %9s\n",
		"Session", "Context", "NewCtx", "Ratio", "K", "Break-even", "10-turn", "100-turn")

	for _, r := range results {
		breakEven, saving10, saving100 := fields(r)
		beStr := "never"
		if breakEven > 0 {
			beStr = fmt.Sprintf("turn %d", breakEven)
		}
		ratio := r.RatioPct()
		fmt.Fprintf(out, "%-10s  %10s  %10s  %5.1f%%  %5.1f  %10s  %7.0f%%  %8.0f%%\n",
			r.ShortID,
			analyzer.FormatNumber(r.ContextTokens),
			analyzer.FormatNumber(r.NewContextTokens),
			ratio,
			r.CallsPerTurn,
			beStr,
			math.Round(saving10),
			math.Round(saving100),
		)
	}
	fmt.Fprintln(out)

	breakEvens := make([]float64, 0, len(results))
	saving10s := make([]float64, len(results))
	saving100s := make([]float64, len(results))
	for i, r := range results {
		breakEven, saving10, saving100 := fields(r)
		if breakEven > 0 {
			breakEvens = append(breakEvens, float64(breakEven))
		}
		saving10s[i] = saving10
		saving100s[i] = saving100
	}

	sort.Float64s(breakEvens)
	sort.Float64s(saving10s)
	sort.Float64s(saving100s)

	beMedian := "never"
	if len(breakEvens) > 0 {
		beMedian = formatMedianTurn(MedianFloat64(breakEvens))
	}

	fmt.Fprintf(out, "Median break-even: %s | 10-turn saving: %.0f%% | 100-turn saving: %.0f%%\n",
		beMedian,
		math.Round(MedianFloat64(saving10s)),
		math.Round(MedianFloat64(saving100s)),
	)
}

// MedianFloat64 returns the median of a sorted slice.
func MedianFloat64(sorted []float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

func formatMedianTurn(turn float64) string {
	if math.Mod(turn, 1) == 0 {
		return fmt.Sprintf("turn %.0f", turn)
	}
	return fmt.Sprintf("turn %.1f", turn)
}
