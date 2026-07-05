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

type rawEntry struct {
	Type          string          `json:"type"`
	Subtype       string          `json:"subtype"`
	Timestamp     string          `json:"timestamp"`
	Message       *rawMessage     `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	Cwd           string          `json:"cwd"`
}

type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`

	TextContent string
	Blocks      []rawContentBlock
	RawContent  string
	Usage       *rawUsage
}

type rawUsage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	OutputTokens             int `json:"output_tokens"`
}

func (m *rawMessage) UnmarshalJSON(data []byte) error {
	var aux struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Usage   *rawUsage       `json:"usage"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	m.Role = aux.Role
	m.Content = aux.Content
	m.Usage = aux.Usage
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
	msg := session.AssistantMessage{
		Text:     m.Text(),
		Thinking: thinking,
		ToolUses: toolUses,
	}
	if m.Usage != nil {
		msg.Usage = &session.Usage{
			InputTokens:              m.Usage.InputTokens,
			CacheCreationInputTokens: m.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     m.Usage.CacheReadInputTokens,
			OutputTokens:             m.Usage.OutputTokens,
		}
	}
	return msg
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

func cleanCwdPaths(text string, cwd string) string {
	if cwd == "" || text == "" {
		return text
	}
	return strings.ReplaceAll(text, cwd, ".")
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
		Text:      cleanCwdPaths(text, e.Cwd),
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
