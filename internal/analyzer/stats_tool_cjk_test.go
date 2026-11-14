package analyzer

import (
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func TestComputeStats_GivenChineseInToolInputAndResult_ThenCJKCharsCountsBoth(t *testing.T) {
	events := []session.Event{
		{
			Kind: session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{
				ToolUses: []session.ToolUse{
					{Name: "Bash", Input: session.ToolInput{Raw: map[string]any{"command": "echo 你好"}}},
				},
			},
		},
		{
			Kind: session.EventToolResult,
			// 5 Han characters plus the fullwidth colon.
			Tool: &session.ToolResult{RawName: "Bash", Success: true, Text: "檔案不存在：x"},
		},
	}

	result := ComputeStats(events)

	const wantCJKChars = 2 + 6
	if got := result.PerTool["Bash"].CJKChars; got != wantCJKChars {
		t.Fatalf("PerTool[Bash].CJKChars = %d, want %d", got, wantCJKChars)
	}
}
