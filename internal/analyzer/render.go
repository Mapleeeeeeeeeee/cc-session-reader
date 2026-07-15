package analyzer

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/formatter"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/skillpath"
)

// RenderOptions holds optional token/API data for RenderStats.
// Zero values mean the data is unavailable.
type RenderOptions struct {
	TranscriptKB   float64
	SessionID      string
	FilteredTokens int
	RawTokens      int
	TokenErr       error
	HasAPIData     bool // true when the caller had context token data from the API
	SkipTokens     bool // true when -no-tokens was requested
}

// RenderStats writes the formatted stats report to out.
func RenderStats(out io.Writer, errOut io.Writer, result StatsResult, opts RenderOptions) {
	fmt.Fprintf(out, "Session: %s\n", opts.SessionID)
	fmt.Fprintf(out, "Transcript: %.1fKB\n\n", opts.TranscriptKB)

	fmt.Fprintln(out, "=== Characters ===")
	fmt.Fprintf(out, "  Raw:      %10s\n", FormatNumber(result.RawChars))
	fmt.Fprintf(out, "  Filtered: %10s\n", FormatNumber(result.FilteredChars))
	if result.RawChars > 0 {
		saved := result.RawChars - result.FilteredChars
		pct := float64(saved) * 100.0 / float64(result.RawChars)
		fmt.Fprintf(out, "  Saved:    %10s (%.1f%%)\n", FormatNumber(saved), pct)
	}

	fmt.Fprintln(out, "\n=== Breakdown ===")
	for _, bl := range []struct{ label, key string }{
		{"KEPT  user text:        ", formatter.CategoryUserText},
		{"KEPT  user answers:     ", formatter.CategoryUserAnswer},
		{"KEPT  assistant text:   ", formatter.CategoryAssistantText},
		{"KEPT  tool summaries:   ", formatter.CategoryToolSummary},
		{"FMT   render overhead:  ", "render_overhead"},
		{"CUT   tool input (raw): ", "tool_input_raw"},
		{"CUT   tool result (raw):", "tool_result_raw"},
		{"CUT   system/noise:     ", "system_noise"},
		{"CUT   command noise:    ", "command_noise"},
	} {
		fmt.Fprintf(out, "  %s %10s\n", bl.label, FormatNumber(result.Categories[bl.key]))
	}

	if len(result.PerTool) > 0 {
		type toolEntry struct {
			name  string
			stats *ToolStats
		}
		var entries []toolEntry
		for name, ts := range result.PerTool {
			entries = append(entries, toolEntry{name, ts})
		}
		sort.Slice(entries, func(i, j int) bool {
			ti := entries[i].stats.InputChars + entries[i].stats.ResultChars
			tj := entries[j].stats.InputChars + entries[j].stats.ResultChars
			if ti != tj {
				return ti > tj
			}
			return entries[i].name < entries[j].name
		})

		fmt.Fprintln(out, "\n=== Per-tool ===")
		maxNameLen := 0
		for _, e := range entries {
			if len(e.name) > maxNameLen {
				maxNameLen = len(e.name)
			}
		}
		for _, e := range entries {
			fmt.Fprintf(out, "  %-*s  %5s calls  %10s input  %10s result\n",
				maxNameLen, e.name,
				FormatNumber(e.stats.CallCount),
				FormatNumber(e.stats.InputChars),
				FormatNumber(e.stats.ResultChars),
			)
		}
	}

	if result.APICallCount > 0 {
		fmt.Fprintln(out, "\n=== Model Context (from API usage) ===")
		fmt.Fprintf(out, "  Last turn context:    %10s\n", FormatNumber(result.LastContextTokens))
		fmt.Fprintf(out, "  Total output:         %10s\n", FormatNumber(result.TotalOutputTokens))
		fmt.Fprintf(out, "  API calls:            %10s\n", FormatNumber(result.APICallCount))
	}

	if opts.SkipTokens {
		return
	}

	fmt.Fprintln(out)
	if opts.HasAPIData {
		if opts.TokenErr == nil {
			saved := result.LastContextTokens - opts.FilteredTokens
			fmt.Fprintln(out, "=== Token Savings ===")
			fmt.Fprintf(out, "  Original context: %10s\n", FormatNumber(result.LastContextTokens))
			fmt.Fprintf(out, "  CLI filtered:     %10s\n", FormatNumber(opts.FilteredTokens))
			if result.LastContextTokens > 0 {
				pct := float64(saved) * 100.0 / float64(result.LastContextTokens)
				fmt.Fprintf(out, "  Saved:            %10s (%.1f%%)\n", FormatNumber(saved), pct)
			}
		} else {
			PrintConfigHint(out)
		}
	} else {
		if opts.TokenErr == nil {
			saved := opts.RawTokens - opts.FilteredTokens
			fmt.Fprintln(out, "=== Tokens (Anthropic API) ===")
			fmt.Fprintf(out, "  Raw:      %10s\n", FormatNumber(opts.RawTokens))
			fmt.Fprintf(out, "  Filtered: %10s\n", FormatNumber(opts.FilteredTokens))
			if opts.RawTokens > 0 {
				pct := float64(saved) * 100.0 / float64(opts.RawTokens)
				fmt.Fprintf(out, "  Saved:    %10s (%.1f%%)\n", FormatNumber(saved), pct)
			}
		} else {
			PrintConfigHint(out)
		}
	}
}

// PrintConfigHint writes the API key setup hint to w.
func PrintConfigHint(w io.Writer) {
	configPath := filepath.Join("~", ".claude", "skills", skillpath.SkillDirName, "config.json")
	if dir, err := skillpath.SkillDir(); err == nil {
		configPath = filepath.Join(dir, "config.json")
	}
	fmt.Fprintf(w, "hint: to see token counts, create %s:\n", configPath)
	fmt.Fprintln(w, `  {"anthropic_api_key_file": "~/.config/anthropic/.env"}`)
}

// FormatNumber formats an integer with comma separators (e.g. 1,234,567).
func FormatNumber(n int) string {
	s := strconv.Itoa(n)
	sign := ""
	digits := s
	if strings.HasPrefix(s, "-") {
		sign = "-"
		digits = s[1:]
	}
	if len(digits) <= 3 {
		return s
	}
	var result []byte
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return sign + string(result)
}
