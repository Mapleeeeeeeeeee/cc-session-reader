package analyzer

import (
	"fmt"
	"strings"

	"claude-code-session-reader/internal/jsonutil"
	"claude-code-session-reader/internal/parser"
	"claude-code-session-reader/internal/summarizer"
)

type AuditResult struct {
	Categories map[string][]string
}

func ComputeAudit(entries []map[string]interface{}) AuditResult {
	categories := map[string][]string{
		"tool_result_cut": {},
		"system_noise":    {},
		"thinking":        {},
	}

	for _, entry := range entries {
		message, ok := entry["message"].(map[string]interface{})
		if !ok {
			continue
		}

		if parser.IsNoise(entry) {
			text := parser.ExtractAllText(entry)
			if strings.TrimSpace(text) != "" {
				entryType := jsonutil.GetStr(entry, "type")
				snippet := truncateStr(text, 200)
				categories["system_noise"] = append(categories["system_noise"],
					fmt.Sprintf("[%s] %s", entryType, snippet))
			}
			continue
		}

		if _, hasToolResult := entry["toolUseResult"]; hasToolResult {
			if !summarizer.IsUserAnswer(entry) {
				text := parser.ExtractAllText(entry)
				if strings.TrimSpace(text) != "" && len(text) > 100 {
					tr, ok := entry["toolUseResult"].(map[string]interface{})
					name := "?"
					if ok {
						if n := jsonutil.GetStr(tr, "commandName"); n != "" {
							name = n
						}
					}
					snippet := truncateStr(text, 300)
					categories["tool_result_cut"] = append(categories["tool_result_cut"],
						fmt.Sprintf("[%s] %s", name, snippet))
				}
			}
			continue
		}

		if jsonutil.GetStr(message, "role") == "assistant" {
			content, ok := message["content"].([]interface{})
			if !ok {
				continue
			}
			for _, item := range content {
				block, isMap := item.(map[string]interface{})
				if !isMap || jsonutil.GetStr(block, "type") != "thinking" {
					continue
				}
				thinking := jsonutil.GetStr(block, "thinking")
				if strings.TrimSpace(thinking) != "" {
					categories["thinking"] = append(categories["thinking"], truncateStr(thinking, 300))
				}
			}
		}
	}

	return AuditResult{Categories: categories}
}

func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}
