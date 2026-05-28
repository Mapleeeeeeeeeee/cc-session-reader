package analyzer

import (
	"fmt"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

type AuditResult struct {
	Categories map[string][]string
}

func ComputeAudit(events []session.Event) AuditResult {
	categories := map[string][]string{
		"tool_result_cut": {},
		"system_noise":    {},
		"thinking":        {},
	}

	for _, event := range events {
		switch event.Kind {
		case session.EventNoise:
			if event.Noise == nil || strings.TrimSpace(event.Noise.Text) == "" {
				continue
			}
			categories["system_noise"] = append(categories["system_noise"],
				fmt.Sprintf("[%s] %s", event.RawType, session.Truncate(event.Noise.Text, 200)))

		case session.EventToolResult:
			if event.Tool == nil || event.User != nil && event.User.IsAnswer {
				continue
			}
			if strings.TrimSpace(event.Tool.Text) != "" && len(event.Tool.Text) > 100 {
				name := event.Tool.RawName
				if name == "" {
					name = "?"
				}
				categories["tool_result_cut"] = append(categories["tool_result_cut"],
					fmt.Sprintf("[%s] %s", name, session.Truncate(event.Tool.Text, 300)))
			}

		case session.EventAssistantMessage:
			if event.Assistant == nil {
				continue
			}
			for _, thinking := range event.Assistant.Thinking {
				if strings.TrimSpace(thinking) != "" {
					categories["thinking"] = append(categories["thinking"], session.Truncate(thinking, 300))
				}
			}
		}
	}

	return AuditResult{Categories: categories}
}
