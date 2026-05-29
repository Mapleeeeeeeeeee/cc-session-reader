// Package analyzer provides stats and audit analysis over normalized session events.
package analyzer

import (
	"strings"
	"unicode/utf8"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/summarizer"
)

type StatsResult struct {
	RawText       string
	FilteredText  string
	RawChars      int
	FilteredChars int
	Categories    map[string]int
}

func ComputeStats(events []session.Event) StatsResult {
	var rawParts, filteredParts []string
	categories := map[string]int{
		"user_text":       0,
		"user_answers":    0,
		"assistant_text":  0,
		"tool_summaries":  0,
		"tool_input_raw":  0,
		"tool_result_raw": 0,
		"system_noise":    0,
		"command_noise":   0,
	}

	for _, event := range events {
		switch event.Kind {
		case session.EventNoise:
			if event.Noise == nil {
				continue
			}
			categories["system_noise"] += utf8.RuneCountInString(event.Noise.Text)
			rawParts = append(rawParts, event.Noise.Text)

		case session.EventUserMessage:
			if event.User == nil {
				continue
			}
			// Command invocation: the short marker is kept content.
			if event.User.CommandMarker != "" {
				categories["user_text"] += utf8.RuneCountInString(event.User.CommandMarker)
				rawParts = append(rawParts, event.User.CommandMarker)
				filteredParts = append(filteredParts, event.User.CommandMarker)
				continue
			}
			// Command output / caveat: machine noise. Count toward raw so the
			// reduction reflects what was actually cut, but never toward
			// filtered — mirrors how system_noise is handled.
			if event.User.IsCommandNoise {
				categories["command_noise"] += utf8.RuneCountInString(event.User.Text)
				rawParts = append(rawParts, event.User.Text)
				continue
			}
			if strings.TrimSpace(event.User.Text) == "" {
				continue
			}
			categories["user_text"] += utf8.RuneCountInString(event.User.Text)
			rawParts = append(rawParts, event.User.Text)
			filteredParts = append(filteredParts, event.User.Text)

		case session.EventAssistantMessage:
			if event.Assistant == nil {
				continue
			}
			if strings.TrimSpace(event.Assistant.Text) != "" {
				categories["assistant_text"] += utf8.RuneCountInString(event.Assistant.Text)
				rawParts = append(rawParts, event.Assistant.Text)
				filteredParts = append(filteredParts, event.Assistant.Text)
			}
			for _, tool := range event.Assistant.ToolUses {
				rawJSON := tool.Input.MarshalNoEscape()
				categories["tool_input_raw"] += utf8.RuneCountInString(rawJSON)
				rawParts = append(rawParts, rawJSON)

				name := tool.Name
				if name == "" {
					name = "?"
				}
				summary := summarizer.SummarizeToolUse(name, tool.Input)
				categories["tool_summaries"] += utf8.RuneCountInString(summary)
				filteredParts = append(filteredParts, summary)
			}

		case session.EventToolResult:
			if event.Tool == nil {
				continue
			}
			if event.User != nil && event.User.IsAnswer {
				categories["user_answers"] += utf8.RuneCountInString(event.User.Text)
				rawParts = append(rawParts, event.Tool.Text)
				filteredParts = append(filteredParts, event.User.Text)
				continue
			}
			categories["tool_result_raw"] += utf8.RuneCountInString(event.Tool.Text)
			rawParts = append(rawParts, event.Tool.Text)
			summary := event.Tool.Summary()
			categories["tool_summaries"] += utf8.RuneCountInString(summary)
			filteredParts = append(filteredParts, summary)
		}
	}

	rawText := strings.Join(rawParts, "\n")
	filteredText := strings.Join(filteredParts, "\n")

	return StatsResult{
		RawText:       rawText,
		FilteredText:  filteredText,
		RawChars:      utf8.RuneCountInString(rawText),
		FilteredChars: utf8.RuneCountInString(filteredText),
		Categories:    categories,
	}
}
