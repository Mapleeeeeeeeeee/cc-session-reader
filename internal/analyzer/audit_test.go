package analyzer

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestComputeAudit_ToolResultCutUsesRuneSafeTruncation(t *testing.T) {
	text := strings.Repeat("a", 299) + "你"
	entries := []map[string]interface{}{
		{
			"type":          "user",
			"toolUseResult": map[string]interface{}{"success": true, "commandName": "Bash"},
			"message": map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type":    "tool_result",
						"content": text,
					},
				},
			},
		},
	}

	result := ComputeAudit(entries)
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
	entries := []map[string]interface{}{
		{
			"type": "system",
			"message": map[string]interface{}{
				"content": "system details",
			},
		},
		{
			"type": "assistant",
			"message": map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{
						"type":     "thinking",
						"thinking": "private reasoning",
					},
				},
			},
		},
	}

	result := ComputeAudit(entries)
	if got := len(result.Categories["system_noise"]); got != 1 {
		t.Fatalf("system_noise count = %d, want 1", got)
	}
	if got := len(result.Categories["thinking"]); got != 1 {
		t.Fatalf("thinking count = %d, want 1", got)
	}
}
