// Package analyzer provides stats and audit analysis over normalized session events.
package analyzer

import (
	"strings"
	"unicode/utf8"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/summarizer"
)

// ToolStats tracks per-tool usage metrics accumulated during ComputeStats.
type ToolStats struct {
	CallCount   int
	InputChars  int
	ResultChars int
}

type StatsResult struct {
	RawText       string
	FilteredText  string
	RawChars      int
	FilteredChars int
	Categories    map[string]int
	PerTool       map[string]*ToolStats

	// Model context baseline derived from API usage fields in the transcript.
	// Model context baseline from API usage fields in the transcript.
	// Zero values mean no usage data was present (older sessions).
	LastContextTokens int
	TotalOutputTokens int
	APICallCount      int
	UserTurnCount     int

	CompactCount int
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
	perTool := map[string]*ToolStats{}
	toolUseNames := map[string]string{}
	seenSkills := map[string]bool{}
	var lastContextTokens, totalOutputTokens, apiCallCount, compactCount, userTurnCount int
	var prevUsage *session.Usage

	for _, event := range events {
		switch event.Kind {
		case session.EventCompactBoundary:
			compactCount++

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
			// Command invocation: the short marker is kept content. Deliberate
			// undercount — the marker counts identically toward raw and filtered,
			// so an invocation contributes zero reduction here. The original
			// invocation wrapper (<command-name>...<command-args>) was already
			// dropped at parse time and never reaches stats, so its ~100 chars of
			// savings are not reflected in the reduction. We accept this: the
			// wrapper is tiny, and the bulk command savings (multi-KB stdout) are
			// correctly captured via the IsCommandNoise branch below, which counts
			// toward raw but not filtered. Surfacing the wrapper would mean
			// re-plumbing the dropped text through the parser for a rounding-error
			// gain — not worth the coupling.
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

			// Harness-injected subtypes: mirror the formatter's compression.
			if event.User.IsSystemReminder || event.User.IsContextUsage {
				categories["user_text"] += utf8.RuneCountInString(event.User.Text)
				rawParts = append(rawParts, event.User.Text)
				continue
			}
			if event.User.IsSkillInjection {
				categories["user_text"] += utf8.RuneCountInString(event.User.Text)
				rawParts = append(rawParts, event.User.Text)
				compact := session.CompactSkillInjection(event.User, seenSkills)
				filteredParts = append(filteredParts, compact)
				continue
			}
			if event.User.IsTeammateMessage {
				categories["user_text"] += utf8.RuneCountInString(event.User.Text)
				rawParts = append(rawParts, event.User.Text)
				if compacted, ok := session.CompactTeammateMessage(event.User.Text); ok {
					filteredParts = append(filteredParts, compacted)
				}
				continue
			}
			if event.User.IsCommandInjection {
				categories["user_text"] += utf8.RuneCountInString(event.User.Text)
				rawParts = append(rawParts, event.User.Text)
				if compacted, ok := session.CompactCommandInjection(event.User.Text); ok {
					filteredParts = append(filteredParts, compacted)
				}
				continue
			}

			userTurnCount++
			categories["user_text"] += utf8.RuneCountInString(event.User.Text)
			rawParts = append(rawParts, event.User.Text)
			if compacted, ok := session.CompactTaskNotification(event.User.Text); ok {
				filteredParts = append(filteredParts, compacted)
			} else {
				filteredParts = append(filteredParts, event.User.Text)
			}

		case session.EventAssistantMessage:
			if event.Assistant == nil {
				continue
			}
			if u := event.Assistant.Usage; u != nil && u.ContextTokens() > 0 && !u.Equal(prevUsage) {
				lastContextTokens = u.ContextTokens()
				totalOutputTokens += u.OutputTokens
				apiCallCount++
				prevUsage = u
			}
			if strings.TrimSpace(event.Assistant.Text) != "" {
				categories["assistant_text"] += utf8.RuneCountInString(event.Assistant.Text)
				rawParts = append(rawParts, event.Assistant.Text)
				filteredParts = append(filteredParts, event.Assistant.Text)
			}
			for _, tool := range event.Assistant.ToolUses {
				rawJSON := tool.Input.MarshalNoEscape()

				name := tool.Name
				if name == "" {
					name = "?"
				}
				toolUseNames[tool.ID] = name

				categories["tool_input_raw"] += utf8.RuneCountInString(rawJSON)
				rawParts = append(rawParts, rawJSON)

				ts := perTool[name]
				if ts == nil {
					ts = &ToolStats{}
					perTool[name] = ts
				}
				ts.CallCount++
				ts.InputChars += utf8.RuneCountInString(rawJSON)

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

			toolName := ""
			if event.Tool.ToolUseID != "" {
				toolName = toolUseNames[event.Tool.ToolUseID]
			}
			if toolName == "" {
				toolName = event.Tool.RawName
			}
			if toolName == "" {
				toolName = "?"
			}
			ts := perTool[toolName]
			if ts == nil {
				ts = &ToolStats{}
				perTool[toolName] = ts
			}
			ts.ResultChars += utf8.RuneCountInString(event.Tool.Text)
		}
	}

	rawText := strings.Join(rawParts, "\n")
	filteredText := strings.Join(filteredParts, "\n")

	return StatsResult{
		RawText:           rawText,
		FilteredText:      filteredText,
		RawChars:          utf8.RuneCountInString(rawText),
		FilteredChars:     utf8.RuneCountInString(filteredText),
		Categories:        categories,
		PerTool:           perTool,
		LastContextTokens: lastContextTokens,
		TotalOutputTokens: totalOutputTokens,
		APICallCount:      apiCallCount,
		UserTurnCount:     userTurnCount,
		CompactCount:      compactCount,
	}
}
