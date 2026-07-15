// Package analyzer provides stats and audit analysis over normalized session events.
package analyzer

import (
	"strings"
	"unicode/utf8"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/formatter"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
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

// ComputeStats walks events once to accumulate RAW-side accounting (every
// byte the transcript contains, and the CUT categories that never reach the
// injected output at all), then measures the FILTERED side by running the
// same events through formatter.RenderReadEventsWithSink — the exact render
// pass `cc-session inherit` uses to inject context, collapsing included. That
// single render pass both produces FilteredText and reports each unit of
// kept content to the KEPT categories, so the two can never drift apart the
// way two independent implementations previously did.
func ComputeStats(events []session.Event) StatsResult {
	var rawParts []string
	categories := map[string]int{
		"user_text":       0,
		"user_answers":    0,
		"assistant_text":  0,
		"tool_summaries":  0,
		"tool_input_raw":  0,
		"tool_result_raw": 0,
		"system_noise":    0,
		"command_noise":   0,
		"render_overhead": 0,
	}
	perTool := map[string]*ToolStats{}
	toolUseNames := map[string]string{}
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
			// Command invocation marker: cheap and identical in both raw and
			// filtered streams, so it contributes no reduction here. Its KEPT
			// weight is measured below via the render pass, not here.
			if event.User.CommandMarker != "" {
				rawParts = append(rawParts, event.User.CommandMarker)
				continue
			}
			// Command output / caveat: machine noise, fully cut from the
			// filtered stream (formatter.renderUserMessage drops it unless
			// -verbose-commands is set, which ComputeStats never sets).
			if event.User.IsCommandNoise {
				categories["command_noise"] += utf8.RuneCountInString(event.User.Text)
				rawParts = append(rawParts, event.User.Text)
				continue
			}
			if strings.TrimSpace(event.User.Text) == "" {
				continue
			}
			// Harness-injected system-reminder/context-usage blocks are
			// dropped entirely by the render pass (never shown), so they are
			// CUT, not KEPT — folded into system_noise since they are the
			// same kind of harness boilerplate as EventNoise.
			if event.User.IsSystemReminder || event.User.IsContextUsage {
				categories["system_noise"] += utf8.RuneCountInString(event.User.Text)
				rawParts = append(rawParts, event.User.Text)
				continue
			}
			// Skill/teammate/command injections are compacted rather than
			// dropped; their KEPT (compacted) size is measured below via the
			// render pass. Only the raw side is recorded here.
			if event.User.IsSkillInjection || event.User.IsTeammateMessage || event.User.IsCommandInjection {
				rawParts = append(rawParts, event.User.Text)
				continue
			}

			userTurnCount++
			rawParts = append(rawParts, event.User.Text)

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
				rawParts = append(rawParts, event.Assistant.Text)
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
			}

		case session.EventToolResult:
			if event.Tool == nil {
				continue
			}
			if event.User != nil && event.User.IsAnswer {
				rawParts = append(rawParts, event.Tool.Text)
				continue
			}
			categories["tool_result_raw"] += utf8.RuneCountInString(event.Tool.Text)
			rawParts = append(rawParts, event.Tool.Text)

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

	// FilteredText is the actual `cc-session inherit`-equivalent render
	// output (agentIDs empty and FormatOptions zero-value, matching
	// inject.RenderFullOutput's defaults), so "Filtered" always means the
	// real injected size. The sink tags each kept unit into categories in
	// the same pass that renders it.
	filteredText, _ := formatter.RenderReadEventsWithSink(events, map[string]bool{}, formatter.FormatOptions{}, func(category, text string) {
		categories[category] += utf8.RuneCountInString(text)
	})
	filteredChars := utf8.RuneCountInString(filteredText)

	// render_overhead is the render pipeline's own structure layered on top
	// of kept content — timestamps, "user:"/"assistant:" labels, tool-line
	// indentation, and blank-line separators — none of which belongs to a
	// single content category but all of which is real injected bytes.
	// Surfacing it explicitly keeps KEPT-category-sum + this line exactly
	// equal to FilteredChars, instead of leaving an unexplained gap.
	kept := categories["user_text"] + categories["user_answers"] + categories["assistant_text"] + categories["tool_summaries"]
	categories["render_overhead"] = filteredChars - kept

	return StatsResult{
		RawText:           rawText,
		FilteredText:      filteredText,
		RawChars:          utf8.RuneCountInString(rawText),
		FilteredChars:     filteredChars,
		Categories:        categories,
		PerTool:           perTool,
		LastContextTokens: lastContextTokens,
		TotalOutputTokens: totalOutputTokens,
		APICallCount:      apiCallCount,
		UserTurnCount:     userTurnCount,
		CompactCount:      compactCount,
	}
}
