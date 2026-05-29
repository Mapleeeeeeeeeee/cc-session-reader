package analyzer

import (
	"strings"
	"testing"

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
