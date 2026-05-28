package analyzer

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func TestComputeAudit_ToolResultCutUsesRuneSafeTruncation(t *testing.T) {
	text := strings.Repeat("a", 299) + "你"
	events := []session.Event{
		{
			Kind: session.EventToolResult,
			Tool: &session.ToolResult{Success: true, RawName: "Bash", Text: text},
		},
	}

	result := ComputeAudit(events)
	items := result.Categories["tool_result_cut"]
	if len(items) != 1 {
		t.Fatalf("tool_result_cut count = %d, want 1", len(items))
	}
	if !utf8.ValidString(items[0]) {
		t.Fatalf("audit sample is not valid UTF-8: %q", items[0])
	}
	if !strings.Contains(items[0], "你") {
		t.Fatalf("audit sample should keep the boundary rune intact, got %q", items[0])
	}
}

func TestComputeAudit_CategorizesSystemNoiseAndThinking(t *testing.T) {
	events := []session.Event{
		{
			Kind:    session.EventNoise,
			RawType: "system",
			Noise:   &session.NoiseEvent{Text: "system details"},
		},
		{
			Kind:      session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{Thinking: []string{"private reasoning"}},
		},
	}

	result := ComputeAudit(events)
	if got := len(result.Categories["system_noise"]); got != 1 {
		t.Fatalf("system_noise count = %d, want 1", got)
	}
	if got := len(result.Categories["thinking"]); got != 1 {
		t.Fatalf("thinking count = %d, want 1", got)
	}
}
