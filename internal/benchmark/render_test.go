package benchmark

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/models"
)

const neverBreaksEven = -1

// Regression: the median break-even left out sessions that never break even, so turn 1, never, never was summarized as "turn 1" (v0.1.84).
func TestPrintCostSummaries_GivenSomeSessionsNeverBreakEven_WhenRendered_ThenMedianCountsThemAsInfinitelyLate(t *testing.T) {
	cases := []struct {
		name       string
		breakEvens []int
		want       string
	}{
		{
			name:       "median lands on never when most sessions never break even",
			breakEvens: []int{1, neverBreaksEven, neverBreaksEven},
			want:       "Median break-even: never (2 of 3 sessions never break even)",
		},
		{
			name:       "median stays a turn when most sessions break even",
			breakEvens: []int{1, 1, neverBreaksEven},
			want:       "Median break-even: turn 1 (1 of 3 sessions never break even)",
		},
		{
			name:       "even count with one never averages into never",
			breakEvens: []int{1, neverBreaksEven},
			want:       "Median break-even: never (1 of 2 sessions never break even)",
		},
		{
			name:       "every session never breaks even",
			breakEvens: []int{neverBreaksEven, neverBreaksEven},
			want:       "Median break-even: never (2 of 2 sessions never break even)",
		},
		{
			name:       "no session is mentioned as never when all break even",
			breakEvens: []int{2, 4, 9},
			want:       "Median break-even: turn 4",
		},
		{
			name:       "even count averages the two middle turns",
			breakEvens: []int{2, 4},
			want:       "Median break-even: turn 3",
		},
	}

	printers := []struct {
		name         string
		print        func(out *bytes.Buffer, results []Result)
		setBreakEven func(r *Result, turn int)
	}{
		{
			name: "cold cache",
			print: func(out *bytes.Buffer, results []Result) {
				PrintCostSummary(out, results, models.Pricing{}, "opus")
			},
			setBreakEven: func(r *Result, turn int) { r.BreakEven = turn },
		},
		{
			name: "warm cache",
			print: func(out *bytes.Buffer, results []Result) {
				PrintWarmCostSummary(out, results, models.Pricing{}, "opus")
			},
			setBreakEven: func(r *Result, turn int) { r.WarmBreakEven = turn },
		},
	}

	for _, printer := range printers {
		for _, tc := range cases {
			t.Run(printer.name+"/"+tc.name, func(t *testing.T) {
				results := make([]Result, len(tc.breakEvens))
				for i, turn := range tc.breakEvens {
					results[i] = Result{ShortID: "aaaaaaaa", ContextTokens: 100_000, NewContextTokens: 50_000}
					printer.setBreakEven(&results[i], turn)
				}

				var out bytes.Buffer
				printer.print(&out, results)

				got, _, _ := strings.Cut(medianBreakEvenLine(t, out.String()), " | ")
				if got != tc.want {
					t.Fatalf("break-even summary = %q, want %q\nfull output:\n%s", got, tc.want, out.String())
				}
			})
		}
	}
}

func medianBreakEvenLine(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "Median break-even:") {
			return line
		}
	}
	t.Fatalf("no median break-even line in output:\n%s", output)
	return ""
}
