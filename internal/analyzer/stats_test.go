package analyzer

import (
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func TestComputeStats_CategorizesRawAndFilteredContent(t *testing.T) {
	events := []session.Event{
		{
			Kind:  session.EventNoise,
			Noise: &session.NoiseEvent{Text: "sys"},
		},
		{
			Kind: session.EventUserMessage,
			User: &session.UserMessage{Text: "hello"},
		},
		{
			Kind: session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{
				Text: "hi",
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
			Tool: &session.ToolResult{Success: true, Text: "ok"},
		},
	}

	result := ComputeStats(events)
	assertCategory(t, result, "system_noise", 3)
	assertCategory(t, result, "user_text", 5)
	assertCategory(t, result, "assistant_text", 2)
	assertCategory(t, result, "tool_input_raw", len([]rune(`{"command":"echo ok","description":"Echo ok"}`)))
	assertCategory(t, result, "tool_result_raw", 2)
	assertCategory(t, result, "user_answers", 0)
	// tool_summaries = "[Bash] Echo ok" (14 runes) + " -> ok: ok" (10 runes) = 24
	assertCategory(t, result, "tool_summaries", 24)
	// RawChars/FilteredChars include "\n" join separators between parts.
	// Raw parts: "sys", "hello", "hi", JSON(45 runes), "ok" → joined = 3+1+5+1+2+1+45+1+2 = 61
	if result.RawChars != 61 {
		t.Fatalf("RawChars = %d, want 61", result.RawChars)
	}
	// Filtered parts: "hello", "hi", "[Bash] Echo ok", " -> ok: ok" → joined = 5+1+2+1+14+1+10 = 34
	if result.FilteredChars != 34 {
		t.Fatalf("FilteredChars = %d, want 34", result.FilteredChars)
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

func assertCategory(t *testing.T, result StatsResult, key string, want int) {
	t.Helper()
	if got := result.Categories[key]; got != want {
		t.Fatalf("category %s = %d, want %d", key, got, want)
	}
}
