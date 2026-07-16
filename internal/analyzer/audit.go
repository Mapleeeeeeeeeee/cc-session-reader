package analyzer

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

// Bucket names for the CUT-content histogram: every unit of transcript text
// that the default render pipeline (formatter.RenderReadEventsWithSink, the
// same pass ComputeStats measures) never surfaces in the injected output.
// BucketOrder lists them from highest risk (content a reader most likely
// wants back) to lowest risk (safe framework noise), so `cc-session audit`
// can print top-to-bottom in descending order of "worry about this".
const (
	BucketFailureOutput   = "failure_output"
	BucketSuccessReadFile = "success_output_read_file"
	BucketSuccessBash     = "success_output_bash"
	BucketSuccessAgent    = "success_output_agent"
	BucketSuccessOther    = "success_output_other"
	BucketToolInput       = "tool_input"
	BucketThinking        = "thinking"
	BucketHarnessNoise    = "harness_noise"
)

// BucketOrder is the canonical risk-descending presentation order for the CUT
// histogram.
var BucketOrder = []string{
	BucketFailureOutput,
	BucketSuccessReadFile,
	BucketSuccessBash,
	BucketSuccessAgent,
	BucketSuccessOther,
	BucketToolInput,
	BucketThinking,
	BucketHarnessNoise,
}

// sampleTruncateRunes bounds each audit sample's displayed length so one huge
// tool result or thinking block can't dominate the printed report.
const sampleTruncateRunes = 300

// FailureLengthStats summarizes the raw character length of every failed
// tool result's original text (ungated by whatever the render pipeline
// would keep). It feeds the "would a keep-short-failures-verbatim budget be
// cheap" decision. Count is zero when the session has no failed tool
// results; Median/P90/Max are meaningless in that case.
type FailureLengthStats struct {
	Count  int
	Median int
	P90    int
	Max    int
}

// AuditResult holds the CUT-content histogram and its samples.
type AuditResult struct {
	// BucketChars is the total CUT character count per bucket, keyed by the
	// Bucket* constants above.
	BucketChars map[string]int
	// Samples holds a handful of representative CUT excerpts per bucket, so
	// callers can print "here's what actually got dropped" alongside the
	// counts. Every sample is already tagged by its map key.
	Samples map[string][]string
	// Failures is the raw-length distribution of failed tool results.
	Failures FailureLengthStats
}

// ComputeAudit walks events once and classifies every unit of CUT content
// (text the default render pipeline drops or shrinks) into the risk-ordered
// buckets above, alongside the raw-length distribution of failed tool
// results.
func ComputeAudit(events []session.Event) AuditResult {
	bucketChars := make(map[string]int, len(BucketOrder))
	samples := make(map[string][]string, len(BucketOrder))
	toolUseNames := map[string]string{}
	var failureLengths []int

	// addSample records chars CUT characters into bucket's total and appends
	// display (a truncated, possibly tool-name-prefixed excerpt) as a sample.
	// chars is always measured from the untruncated raw content, never from
	// display, so truncation/prefixing never distorts the histogram totals.
	addSample := func(bucket string, chars int, display string) {
		bucketChars[bucket] += chars
		samples[bucket] = append(samples[bucket], display)
	}

	for _, event := range events {
		switch event.Kind {
		case session.EventNoise:
			if event.Noise == nil || strings.TrimSpace(event.Noise.Text) == "" {
				continue
			}
			addSample(BucketHarnessNoise, utf8.RuneCountInString(event.Noise.Text),
				fmt.Sprintf("[%s] %s", event.RawType, session.Truncate(event.Noise.Text, sampleTruncateRunes)))

		case session.EventUserMessage:
			if event.User == nil || !isHarnessNoiseMessage(event.User) {
				continue
			}
			if strings.TrimSpace(event.User.Text) == "" {
				continue
			}
			addSample(BucketHarnessNoise, utf8.RuneCountInString(event.User.Text),
				session.Truncate(event.User.Text, sampleTruncateRunes))

		case session.EventAssistantMessage:
			if event.Assistant == nil {
				continue
			}
			for _, thinking := range event.Assistant.Thinking {
				if strings.TrimSpace(thinking) == "" {
					continue
				}
				addSample(BucketThinking, utf8.RuneCountInString(thinking), session.Truncate(thinking, sampleTruncateRunes))
			}
			for _, tool := range event.Assistant.ToolUses {
				name := tool.Name
				if name == "" {
					name = "?"
				}
				toolUseNames[tool.ID] = name

				rawJSON := tool.Input.MarshalNoEscape()
				addSample(BucketToolInput, utf8.RuneCountInString(rawJSON),
					fmt.Sprintf("[%s] %s", name, session.Truncate(rawJSON, sampleTruncateRunes)))
			}

		case session.EventToolResult:
			if event.Tool == nil || (event.User != nil && event.User.IsAnswer) {
				continue
			}
			text := event.Tool.Text
			if strings.TrimSpace(text) == "" {
				continue
			}

			if !event.Tool.Success {
				failureLengths = append(failureLengths, utf8.RuneCountInString(text))
			}

			// cutChars approximates how much of this result's raw text never
			// reaches the reader. result.Summary() is exactly what the render
			// pipeline keeps (formatter/render.go calls the same method), so
			// raw-minus-summary is the CUT remainder — modulo the few bytes
			// Summary() spends on its own " -> ok"/" -> FAILED" prefix, which
			// isn't "kept" raw content either. That makes this a slightly
			// conservative (never-overcounts-CUT) estimate, not an exact one.
			cutChars := utf8.RuneCountInString(text) - utf8.RuneCountInString(event.Tool.Summary())
			if cutChars <= 0 {
				continue
			}

			toolName := event.Tool.RawName
			if toolName == "" {
				toolName = toolUseNames[event.Tool.ToolUseID]
			}
			if toolName == "" {
				toolName = "?"
			}

			bucket := resultBucket(event.Tool.Success, toolName)
			addSample(bucket, cutChars, fmt.Sprintf("[%s] %s", toolName, session.Truncate(text, sampleTruncateRunes)))
		}
	}

	return AuditResult{
		BucketChars: bucketChars,
		Samples:     samples,
		Failures:    computeFailureLengthStats(failureLengths),
	}
}

// isHarnessNoiseMessage reports whether user carries one of the harness-
// injected subtypes that formatter's render pipeline drops entirely or
// compacts to a fraction of its raw size (system-reminder, context-usage,
// command injection, teammate warnings, and machine command output) — the
// "safe to lose" bucket. Skill injections are excluded: they inject real
// SKILL.md instructions the user opted into, not framework boilerplate, so
// lumping them in here would misrepresent them as noise.
func isHarnessNoiseMessage(user *session.UserMessage) bool {
	return user.IsSystemReminder || user.IsContextUsage || user.IsCommandInjection ||
		user.IsTeammateMessage || user.IsCommandNoise
}

// resultBucket classifies a tool result into its CUT bucket. Failed results
// always land in BucketFailureOutput regardless of tool; successful results
// split by family using the same {Read, Write, Edit, Agent} vs. everything-
// else grouping that session.ToolResult.Summary() already uses to decide
// between a bare "-> ok" and a first-line excerpt (ADR-002).
func resultBucket(success bool, toolName string) string {
	if !success {
		return BucketFailureOutput
	}
	switch toolName {
	case session.ToolRead, session.ToolWrite, session.ToolEdit:
		return BucketSuccessReadFile
	case session.ToolBash:
		return BucketSuccessBash
	case session.ToolAgent:
		return BucketSuccessAgent
	default:
		return BucketSuccessOther
	}
}

// computeFailureLengthStats returns the zero-value FailureLengthStats for an
// empty input (no failures in the session).
func computeFailureLengthStats(lengths []int) FailureLengthStats {
	if len(lengths) == 0 {
		return FailureLengthStats{}
	}
	sorted := append([]int(nil), lengths...)
	sort.Ints(sorted)
	return FailureLengthStats{
		Count:  len(sorted),
		Median: percentile(sorted, 0.5),
		P90:    percentile(sorted, 0.9),
		Max:    sorted[len(sorted)-1],
	}
}

// percentile returns the p-th percentile (0 < p <= 1) of sorted (ascending,
// non-empty) using the nearest-rank method: rank = ceil(p*n), 1-indexed.
// Nearest-rank avoids interpolation complexity that isn't warranted for the
// small failure counts a single session typically has.
func percentile(sorted []int, p float64) int {
	n := len(sorted)
	idx := int(math.Ceil(p*float64(n))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}
