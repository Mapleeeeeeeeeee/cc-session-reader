package claudecodec

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

var userAnswerPrefixes = []string{
	"User has answered your questions:",
	"Your questions have been answered:",
}

// Command-related tags that Claude Code embeds in user-role transcript entries.
// Invocation tags carry a marker; output and caveat tags are command noise.
const (
	tagCommandNameOpen  = "<command-name>"
	tagCommandNameClose = "</command-name>"
	tagBashInputOpen    = "<bash-input>"
	tagBashInputClose   = "</bash-input>"
	tagLocalStdout      = "<local-command-stdout>"
	tagBashStdout       = "<bash-stdout>"
	tagBashStderr       = "<bash-stderr>"
	tagLocalCaveat      = "<local-command-caveat>"
)

// bangCommandMarkerMaxRunes caps the bang-command text rendered inside the
// "[!...]" marker so a long one-liner does not blow up the marker line.
const bangCommandMarkerMaxRunes = 80

// classifyCommandUserMessage inspects a user-role message body and returns a
// classified UserMessage when the body is a slash/bang command invocation or
// command output. It returns nil for ordinary typed messages so the caller
// falls back to plain user-message handling.
//
// Single source of truth: detection lives here in the parser layer so the
// formatter and stats consumers branch on domain fields, never re-match tags.
func classifyCommandUserMessage(text string) *session.UserMessage {
	trimmed := strings.TrimSpace(text)

	// Caveat is pure boilerplate — always droppable, even in verbose mode.
	if strings.HasPrefix(trimmed, tagLocalCaveat) {
		return &session.UserMessage{IsCommandNoise: true, IsCaveat: true}
	}

	// Slash-command invocation: extract "/context" -> marker "[/context]".
	if name := extractBetween(trimmed, tagCommandNameOpen, tagCommandNameClose); name != "" {
		return &session.UserMessage{CommandMarker: "[" + strings.TrimSpace(name) + "]"}
	}

	// Bang-command invocation: extract the command -> marker "[!CMD]".
	if cmd := extractBetween(trimmed, tagBashInputOpen, tagBashInputClose); strings.TrimSpace(cmd) != "" {
		oneLine := collapseWhitespace(cmd)
		return &session.UserMessage{CommandMarker: "[!" + session.Truncate(oneLine, bangCommandMarkerMaxRunes) + "]"}
	}

	// Command output (slash stdout, bash stdout/stderr): droppable body,
	// surfaced only under -verbose-commands with ANSI stripped at render time.
	if strings.HasPrefix(trimmed, tagLocalStdout) ||
		strings.HasPrefix(trimmed, tagBashStdout) ||
		strings.HasPrefix(trimmed, tagBashStderr) {
		return &session.UserMessage{IsCommandNoise: true, Text: trimmed}
	}

	return nil
}

// extractBetween returns the substring between the first open and the next
// close tag, or "" if either tag is absent.
func extractBetween(s, open, close string) string {
	start := strings.Index(s, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(s[start:], close)
	if end < 0 {
		return ""
	}
	return s[start : start+end]
}

// collapseWhitespace folds runs of whitespace (including newlines) into single
// spaces so a multi-line bang command renders as one marker line.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

type rawEntry struct {
	Type          string          `json:"type"`
	Timestamp     string          `json:"timestamp"`
	Message       *rawMessage     `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
}

type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`

	TextContent string
	Blocks      []rawContentBlock
	RawContent  string
}

func (m *rawMessage) UnmarshalJSON(data []byte) error {
	var aux struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	m.Role = aux.Role
	m.Content = aux.Content
	if len(aux.Content) == 0 {
		return nil
	}
	if err := json.Unmarshal(aux.Content, &m.TextContent); err == nil {
		return nil
	}
	if err := json.Unmarshal(aux.Content, &m.Blocks); err == nil {
		return nil
	}
	m.RawContent = marshalNoEscape(aux.Content)
	return nil
}

func (m rawMessage) Text() string {
	if m.TextContent != "" {
		return m.TextContent
	}
	if m.RawContent != "" {
		return m.RawContent
	}
	var parts []string
	for _, block := range m.Blocks {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (m rawMessage) Assistant() session.AssistantMessage {
	var thinking []string
	var toolUses []session.ToolUse
	for _, block := range m.Blocks {
		switch block.Type {
		case "thinking":
			if strings.TrimSpace(block.Thinking) != "" {
				thinking = append(thinking, block.Thinking)
			}
		case "tool_use":
			input := map[string]any{}
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &input)
			}
			toolUses = append(toolUses, session.ToolUse{
				ID:    block.ID,
				Name:  block.Name,
				Input: session.ToolInput{Raw: input},
			})
		}
	}
	return session.AssistantMessage{
		Text:     m.Text(),
		Thinking: thinking,
		ToolUses: toolUses,
	}
}

type rawContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Name      string          `json:"name"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
}

func (e rawEntry) toToolResult() session.ToolResult {
	result := rawToolUseResult{Success: true}
	if len(e.ToolUseResult) > 0 {
		_ = json.Unmarshal(e.ToolUseResult, &result)
	}
	text, toolUseID := extractToolResultText(e.Message.Blocks)
	name := result.CommandName
	if name == "" {
		name = result.AgentType
	}
	return session.ToolResult{
		ToolUseID: toolUseID,
		Success:   result.Success,
		Text:      text,
		RawName:   name,
	}
}

type rawToolUseResult struct {
	Success     bool   `json:"success"`
	CommandName string `json:"commandName"`
	AgentType   string `json:"agentType"`
}

func extractToolResultText(blocks []rawContentBlock) (string, string) {
	for _, block := range blocks {
		if block.Type != "tool_result" {
			continue
		}
		if len(block.Content) == 0 {
			return "", block.ToolUseID
		}
		var s string
		if err := json.Unmarshal(block.Content, &s); err == nil {
			return s, block.ToolUseID
		}
		var subBlocks []rawContentBlock
		if err := json.Unmarshal(block.Content, &subBlocks); err == nil {
			var parts []string
			for _, subBlock := range subBlocks {
				if subBlock.Type == "text" && subBlock.Text != "" {
					parts = append(parts, subBlock.Text)
				}
			}
			return strings.Join(parts, "\n"), block.ToolUseID
		}
		return string(block.Content), block.ToolUseID
	}
	return "", ""
}

func extractUserAnswer(blocks []rawContentBlock) string {
	text, _ := extractToolResultText(blocks)
	for _, prefix := range userAnswerPrefixes {
		if strings.HasPrefix(text, prefix) {
			return text
		}
	}
	return ""
}

func (e rawEntry) extractAllText() string {
	var parts []string
	if e.Message != nil {
		if text := e.Message.Text(); text != "" {
			parts = append(parts, text)
		}
		for _, block := range e.Message.Blocks {
			switch block.Type {
			case "tool_use":
				if len(block.Input) > 0 {
					parts = append(parts, marshalNoEscape(block.Input))
				}
			case "tool_result":
				text, _ := extractToolResultText([]rawContentBlock{block})
				if text != "" {
					parts = append(parts, text)
				}
			case "thinking":
				if block.Thinking != "" {
					parts = append(parts, block.Thinking)
				}
			}
		}
	}

	var tr map[string]any
	if len(e.ToolUseResult) > 0 && json.Unmarshal(e.ToolUseResult, &tr) == nil {
		// stdout/stderr/output are CLI results; content covers file-based tool outputs.
		for _, key := range []string{"stdout", "stderr", "output", "content"} {
			if v, ok := tr[key]; ok && v != nil {
				parts = append(parts, fmt.Sprintf("%v", v))
			}
		}
	}
	return strings.Join(parts, "\n")
}

func marshalNoEscape(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return session.MarshalNoEscape(v)
}
