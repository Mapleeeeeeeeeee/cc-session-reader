package analyzer

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

// TestComputeStats_SeparatesRawFromFilteredByContent verifies the core
// categorization contract: human-readable content (user/assistant text and
// tool *summaries*) flows into FilteredText, while verbose machine content
// (raw tool-input JSON, raw tool-result bodies, system noise) flows only into
// RawText. We assert on *which stream contains which content* rather than on
// the byte arithmetic of the join, so that changing a summary template's
// wording (a real user-visible change) is caught by substring assertions
// while a benign reformat of the join does not falsely fail the test.
func TestComputeStats_SeparatesRawFromFilteredByContent(t *testing.T) {
	events := []session.Event{
		{
			Kind:  session.EventNoise,
			Noise: &session.NoiseEvent{Text: "sys-noise-body"},
		},
		{
			Kind: session.EventUserMessage,
			User: &session.UserMessage{Text: "hello user"},
		},
		{
			Kind: session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{
				Text: "hi there",
				ToolUses: []session.ToolUse{
					{
						Name:  "Bash",
						Input: session.ToolInput{Raw: map[string]any{"command": "echo ok", "description": "Echo ok"}},
					},
				},
			},
		},
		{
			Kind: session.EventToolResult,
			Tool: &session.ToolResult{Success: true, Text: "tool-result-body"},
		},
	}

	result := ComputeStats(events)

	// FilteredText is the human-facing stream: user/assistant text plus the
	// short tool summaries the user sees in read/context output.
	assertContains(t, "FilteredText", result.FilteredText, "hello user")
	assertContains(t, "FilteredText", result.FilteredText, "hi there")
	assertContains(t, "FilteredText", result.FilteredText, "[Bash] Echo ok")
	assertContains(t, "FilteredText", result.FilteredText, " -> ok: tool-result-body")
	// Verbose raw content must NOT leak into the filtered stream.
	assertNotContains(t, "FilteredText", result.FilteredText, "sys-noise-body")
	assertNotContains(t, "FilteredText", result.FilteredText, `"command"`)

	// RawText is the verbose stream: it keeps the original JSON, the raw
	// result body, and system noise.
	assertContains(t, "RawText", result.RawText, "sys-noise-body")
	assertContains(t, "RawText", result.RawText, `"command":"echo ok"`)
	assertContains(t, "RawText", result.RawText, "tool-result-body")

	// Category counts assert the *routing* of each content kind into the right
	// bucket. Each expected value is the obvious rune count of one input field,
	// not a transcription of the summarizer/join templates.
	assertCategory(t, result, "system_noise", len([]rune("sys-noise-body")))
	assertCategory(t, result, "user_text", len([]rune("hello user")))
	assertCategory(t, result, "assistant_text", len([]rune("hi there")))
	assertCategory(t, result, "tool_input_raw", len([]rune(`{"command":"echo ok","description":"Echo ok"}`)))
	assertCategory(t, result, "tool_result_raw", len([]rune("tool-result-body")))
	assertCategory(t, result, "user_answers", 0)
}

// TestComputeStats_CountsCharsForSingleUserMessage pins the exact char-count
// arithmetic on a case trivial enough to verify by eye: a single ASCII user
// message becomes both the entire raw and filtered stream with no join
// separators and no summarizer formatting involved.
func TestComputeStats_CountsCharsForSingleUserMessage(t *testing.T) {
	const message = "hello" // 5 ASCII runes, the whole stream

	events := []session.Event{
		{
			Kind: session.EventUserMessage,
			User: &session.UserMessage{Text: message},
		},
	}

	result := ComputeStats(events)

	if result.RawChars != 5 {
		t.Fatalf("RawChars = %d, want 5", result.RawChars)
	}
	if result.FilteredChars != 5 {
		t.Fatalf("FilteredChars = %d, want 5", result.FilteredChars)
	}
	if result.RawText != message {
		t.Fatalf("RawText = %q, want %q", result.RawText, message)
	}
	if result.FilteredText != message {
		t.Fatalf("FilteredText = %q, want %q", result.FilteredText, message)
	}
}

func TestComputeStats_UserAnswerIsKeptAsUserAnswer(t *testing.T) {
	events := []session.Event{
		{
			Kind: session.EventToolResult,
			Tool: &session.ToolResult{Success: true, Text: "User has answered your questions: yes"},
			User: &session.UserMessage{Text: "User has answered your questions: yes", IsAnswer: true},
		},
	}

	result := ComputeStats(events)
	assertCategory(t, result, "user_answers", len([]rune("User has answered your questions: yes")))
	assertCategory(t, result, "tool_result_raw", 0)
}

// TestComputeStats_CommandNoiseCountsRawNotFiltered verifies command output
// and caveat bodies are routed to the command_noise cut bucket: counted in raw
// (so reduction reflects the real cut) but kept out of user_text and the
// filtered stream. Guards against the regression where command machine output
// inflated user_text and made reduction look artificially low.
func TestComputeStats_CommandNoiseCountsRawNotFiltered(t *testing.T) {
	const stdoutBody = "Context Usage 30k/200k tokens" // command stdout, must be cut
	const caveatBody = "Caveat: DO NOT respond"        // caveat, must be cut

	events := []session.Event{
		{Kind: session.EventUserMessage, User: &session.UserMessage{
			IsCommandNoise: true, Text: stdoutBody,
		}},
		{Kind: session.EventUserMessage, User: &session.UserMessage{
			IsCommandNoise: true, IsCaveat: true, Text: caveatBody,
		}},
	}

	result := ComputeStats(events)

	wantNoise := len([]rune(stdoutBody)) + len([]rune(caveatBody))
	assertCategory(t, result, "command_noise", wantNoise)
	// Must not leak into the kept user-text bucket.
	assertCategory(t, result, "user_text", 0)

	assertContains(t, "RawText", result.RawText, stdoutBody)
	assertContains(t, "RawText", result.RawText, caveatBody)
	assertNotContains(t, "FilteredText", result.FilteredText, stdoutBody)
	assertNotContains(t, "FilteredText", result.FilteredText, caveatBody)
}

// TestComputeStats_CommandMarkerIsKeptContent verifies the short invocation
// marker is treated as kept user content: it appears in both streams and is
// counted under user_text, never command_noise.
func TestComputeStats_CommandMarkerIsKeptContent(t *testing.T) {
	const marker = "[/context]"
	events := []session.Event{
		{Kind: session.EventUserMessage, User: &session.UserMessage{CommandMarker: marker}},
	}

	result := ComputeStats(events)

	assertCategory(t, result, "user_text", len([]rune(marker)))
	assertCategory(t, result, "command_noise", 0)
	assertContains(t, "FilteredText", result.FilteredText, marker)
	assertContains(t, "RawText", result.RawText, marker)
}

func assertCategory(t *testing.T, result StatsResult, key string, want int) {
	t.Helper()
	if got := result.Categories[key]; got != want {
		t.Fatalf("category %s = %d, want %d", key, got, want)
	}
}

func assertContains(t *testing.T, streamName, stream, want string) {
	t.Helper()
	if !strings.Contains(stream, want) {
		t.Fatalf("%s = %q, want it to contain %q", streamName, stream, want)
	}
}

func assertNotContains(t *testing.T, streamName, stream, unwanted string) {
	t.Helper()
	if strings.Contains(stream, unwanted) {
		t.Fatalf("%s = %q, want it NOT to contain %q", streamName, stream, unwanted)
	}
}

// TestComputeStats_GivenMultipleToolTypes_TracksPerToolBreakdown verifies that
// PerTool accumulates CallCount and InputChars from tool uses and ResultChars
// from tool results, keyed by tool name, without disturbing category totals.
func TestComputeStats_GivenMultipleToolTypes_TracksPerToolBreakdown(t *testing.T) {
	bashInput := session.ToolInput{Raw: map[string]any{"command": "ls"}}
	readInput := session.ToolInput{Raw: map[string]any{"file_path": "/tmp/x"}}
	const bashResultText = "file1.txt\nfile2.txt"
	const readResultText = "contents of file"

	events := []session.Event{
		{
			Kind: session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{
				ToolUses: []session.ToolUse{
					{Name: "Bash", Input: bashInput},
					{Name: "Read", Input: readInput},
				},
			},
		},
		{
			Kind: session.EventToolResult,
			Tool: &session.ToolResult{RawName: "Bash", Success: true, Text: bashResultText},
		},
		{
			Kind: session.EventToolResult,
			Tool: &session.ToolResult{RawName: "Read", Success: true, Text: readResultText},
		},
	}

	result := ComputeStats(events)

	bashInputChars := utf8.RuneCountInString(bashInput.MarshalNoEscape())
	readInputChars := utf8.RuneCountInString(readInput.MarshalNoEscape())

	if result.PerTool["Bash"] == nil {
		t.Fatal("PerTool[Bash] is nil, want entry")
	}
	if got := result.PerTool["Bash"].CallCount; got != 1 {
		t.Fatalf("PerTool[Bash].CallCount = %d, want 1", got)
	}
	if got := result.PerTool["Bash"].InputChars; got != bashInputChars {
		t.Fatalf("PerTool[Bash].InputChars = %d, want %d", got, bashInputChars)
	}
	if got := result.PerTool["Bash"].ResultChars; got != utf8.RuneCountInString(bashResultText) {
		t.Fatalf("PerTool[Bash].ResultChars = %d, want %d", got, utf8.RuneCountInString(bashResultText))
	}

	if result.PerTool["Read"] == nil {
		t.Fatal("PerTool[Read] is nil, want entry")
	}
	if got := result.PerTool["Read"].CallCount; got != 1 {
		t.Fatalf("PerTool[Read].CallCount = %d, want 1", got)
	}
	if got := result.PerTool["Read"].InputChars; got != readInputChars {
		t.Fatalf("PerTool[Read].InputChars = %d, want %d", got, readInputChars)
	}
	if got := result.PerTool["Read"].ResultChars; got != utf8.RuneCountInString(readResultText) {
		t.Fatalf("PerTool[Read].ResultChars = %d, want %d", got, utf8.RuneCountInString(readResultText))
	}

	// Category-level totals must be unchanged by per-tool tracking.
	wantInputRaw := bashInputChars + readInputChars
	assertCategory(t, result, "tool_input_raw", wantInputRaw)
	assertCategory(t, result, "tool_result_raw", utf8.RuneCountInString(bashResultText)+utf8.RuneCountInString(readResultText))
}

// TestComputeStats_GivenNoTools_ReturnsEmptyPerToolMap verifies that sessions
// without any tool interaction produce an empty PerTool map rather than nil.
func TestComputeStats_GivenNoTools_ReturnsEmptyPerToolMap(t *testing.T) {
	events := []session.Event{
		{
			Kind: session.EventUserMessage,
			User: &session.UserMessage{Text: "just a plain message"},
		},
	}

	result := ComputeStats(events)

	if result.PerTool == nil {
		t.Fatal("PerTool is nil, want empty map")
	}
	if got := len(result.PerTool); got != 0 {
		t.Fatalf("len(PerTool) = %d, want 0", got)
	}
}

// TestComputeStats_GivenToolResultWithRawName_MatchesResultToToolName verifies
// that ResultChars are attributed to the tool named by RawName even when there
// is no corresponding tool use in the same session. CallCount stays zero
// because call counts are driven by tool uses, not results.
func TestComputeStats_GivenToolResultWithRawName_MatchesResultToToolName(t *testing.T) {
	const editResultText = "3 changes applied"
	events := []session.Event{
		{
			Kind: session.EventToolResult,
			Tool: &session.ToolResult{RawName: "Edit", Success: true, Text: editResultText},
		},
	}

	result := ComputeStats(events)

	if result.PerTool["Edit"] == nil {
		t.Fatal("PerTool[Edit] is nil, want entry")
	}
	wantResultChars := utf8.RuneCountInString(editResultText)
	if got := result.PerTool["Edit"].ResultChars; got != wantResultChars {
		t.Fatalf("PerTool[Edit].ResultChars = %d, want %d", got, wantResultChars)
	}
	// No tool use was present, so CallCount must be zero.
	if got := result.PerTool["Edit"].CallCount; got != 0 {
		t.Fatalf("PerTool[Edit].CallCount = %d, want 0 (no tool use, only result)", got)
	}
}

// TestComputeStats_GivenUsageData_ComputesModelBaseline verifies that usage
// fields on assistant messages are aggregated into LastContextTokens (last turn),
// TotalOutputTokens (sum), and APICallCount.
func TestComputeStats_GivenUsageData_ComputesModelBaseline(t *testing.T) {
	events := []session.Event{
		{
			Kind: session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{
				Text: "first reply",
				Usage: &session.Usage{
					InputTokens:              100,
					CacheCreationInputTokens: 200,
					CacheReadInputTokens:     300,
					OutputTokens:             50,
				},
			},
		},
		{
			Kind: session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{
				Text: "second reply",
				Usage: &session.Usage{
					InputTokens:              500,
					CacheCreationInputTokens: 0,
					CacheReadInputTokens:     800,
					OutputTokens:             120,
				},
			},
		},
		{
			Kind: session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{
				Text: "third reply",
				Usage: &session.Usage{
					InputTokens:              50,
					CacheCreationInputTokens: 10,
					CacheReadInputTokens:     20,
					OutputTokens:             30,
				},
			},
		},
	}

	result := ComputeStats(events)

	// Turn 1 context: 100+200+300 = 600
	// Turn 2 context: 500+0+800   = 1300
	// Turn 3 context: 50+10+20    = 80   <- last turn
	if got := result.LastContextTokens; got != 80 {
		t.Fatalf("LastContextTokens = %d, want 80 (last turn)", got)
	}
	// Total output: 50+120+30 = 200
	if got := result.TotalOutputTokens; got != 200 {
		t.Fatalf("TotalOutputTokens = %d, want 200", got)
	}
	if got := result.APICallCount; got != 3 {
		t.Fatalf("APICallCount = %d, want 3", got)
	}
}

// Guards against the double-counting bug: Claude Code writes two JSONL entries
// per assistant turn (streaming partial + completion) with identical usage.
// Consecutive entries with equal usage must be counted only once.
func TestComputeStats_GivenConsecutiveDuplicateUsage_ThenCountsOnce(t *testing.T) {
	dup := &session.Usage{
		InputTokens:              10,
		CacheCreationInputTokens: 500,
		CacheReadInputTokens:     200,
		OutputTokens:             80,
	}
	events := []session.Event{
		{
			Kind:      session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{Text: "partial", Usage: dup},
		},
		{
			Kind:      session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{Text: "full reply", Usage: dup},
		},
		{
			Kind: session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{
				Text: "different turn",
				Usage: &session.Usage{
					InputTokens:              20,
					CacheCreationInputTokens: 100,
					CacheReadInputTokens:     600,
					OutputTokens:             50,
				},
			},
		},
	}

	result := ComputeStats(events)

	if result.APICallCount != 2 {
		t.Fatalf("APICallCount = %d, want 2 (duplicate pair counted once + one distinct)", result.APICallCount)
	}
	if result.TotalOutputTokens != 130 {
		t.Fatalf("TotalOutputTokens = %d, want 130 (80 + 50)", result.TotalOutputTokens)
	}
	if result.LastContextTokens != 720 {
		t.Fatalf("LastContextTokens = %d, want 720 (20+100+600)", result.LastContextTokens)
	}
}

// Guards against synthetic model entries inflating counts: entries with
// all-zero usage (ContextTokens()==0) should be excluded.
func TestComputeStats_GivenZeroTokenUsage_ThenExcludedFromBaseline(t *testing.T) {
	events := []session.Event{
		{
			Kind: session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{
				Text:  "synthetic",
				Usage: &session.Usage{InputTokens: 0, CacheCreationInputTokens: 0, CacheReadInputTokens: 0, OutputTokens: 0},
			},
		},
		{
			Kind: session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{
				Text: "real",
				Usage: &session.Usage{
					InputTokens:              5,
					CacheCreationInputTokens: 100,
					CacheReadInputTokens:     200,
					OutputTokens:             30,
				},
			},
		},
	}

	result := ComputeStats(events)

	if result.APICallCount != 1 {
		t.Fatalf("APICallCount = %d, want 1 (zero-token entry excluded)", result.APICallCount)
	}
	if result.TotalOutputTokens != 30 {
		t.Fatalf("TotalOutputTokens = %d, want 30 (zero-token entry excluded)", result.TotalOutputTokens)
	}
}

// TestComputeStats_GivenNoUsageData_ZeroModelBaseline guards backward
// compatibility: sessions from before usage fields were recorded must produce
// zero model baseline values rather than panicking or returning garbage.
func TestComputeStats_GivenNoUsageData_ZeroModelBaseline(t *testing.T) {
	events := []session.Event{
		{
			Kind:      session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{Text: "reply without usage"},
		},
		{
			Kind: session.EventUserMessage,
			User: &session.UserMessage{Text: "hello"},
		},
	}

	result := ComputeStats(events)

	if result.LastContextTokens != 0 {
		t.Fatalf("LastContextTokens = %d, want 0 (no usage data)", result.LastContextTokens)
	}
	if result.TotalOutputTokens != 0 {
		t.Fatalf("TotalOutputTokens = %d, want 0 (no usage data)", result.TotalOutputTokens)
	}
	if result.APICallCount != 0 {
		t.Fatalf("APICallCount = %d, want 0 (no usage data)", result.APICallCount)
	}
}
