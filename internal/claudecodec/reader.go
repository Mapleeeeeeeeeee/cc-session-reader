// Package claudecodec converts Claude Code transcript JSONL into session events.
package claudecodec

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

// noiseTypes are entry types that carry no user/assistant conversation content
// and are filtered out during session parsing. These include metadata entries
// (ai-title, custom-title, agent-name, mode, permission-mode), infrastructure
// signals (bridge-session, queue-operation, progress, system), and large
// payloads irrelevant to conversation flow (file-history-snapshot, attachment).
var noiseTypes = map[string]bool{
	"file-history-snapshot": true,
	"attachment":            true,
	"bridge-session":        true,
	"last-prompt":           true,
	"permission-mode":       true,
	"mode":                  true,
	"ai-title":              true,
	"custom-title":          true,
	"agent-name":            true,
	"pr-link":               true,
	"queue-operation":       true,
	"progress":              true,
	"system":                true,
}

func ReadFile(path string, handle func(session.Event) error) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) == 0 && readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.EOF {
			return fmt.Errorf("read transcript: %w", readErr)
		}
		if len(bytes.TrimSpace(line)) == 0 {
			if readErr == io.EOF {
				break
			}
			continue
		}
		event, ok, parseErr := ParseLine(line)
		if parseErr != nil {
			return parseErr
		}
		if ok {
			if err := handle(event); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
	}
	return nil
}

func ReadAll(path string) ([]session.Event, error) {
	var events []session.Event
	err := ReadFile(path, func(event session.Event) error {
		events = append(events, event)
		return nil
	})
	return events, err
}

func ParseLine(line []byte) (session.Event, bool, error) {
	var raw rawEntry
	if err := json.Unmarshal(line, &raw); err != nil {
		return session.Event{}, false, fmt.Errorf("parse transcript line: %w", err)
	}
	event := session.Event{
		Timestamp: raw.Timestamp,
		RawType:   raw.Type,
	}

	if raw.Message == nil {
		if noiseTypes[raw.Type] {
			event.Kind = session.EventNoise
			event.Noise = &session.NoiseEvent{Text: raw.extractAllText()}
			return event, true, nil
		}
		return session.Event{}, false, nil
	}

	if noiseTypes[raw.Type] {
		event.Kind = session.EventNoise
		event.Noise = &session.NoiseEvent{Text: raw.extractAllText()}
		return event, true, nil
	}

	if len(raw.ToolUseResult) > 0 {
		toolResult := raw.toToolResult()
		event.Kind = session.EventToolResult
		event.Tool = &toolResult
		if answer := extractUserAnswer(raw.Message.Blocks); answer != "" {
			event.User = &session.UserMessage{Text: answer, IsAnswer: true}
		}
		return event, true, nil
	}

	switch raw.Message.Role {
	case "user":
		text := raw.Message.Text()
		if strings.TrimSpace(text) == "" {
			return session.Event{}, false, nil
		}
		event.Kind = session.EventUserMessage
		if classified := classifyCommandUserMessage(text); classified != nil {
			event.User = classified
		} else {
			event.User = &session.UserMessage{Text: text}
		}
		return event, true, nil
	case "assistant":
		assistant := raw.Message.Assistant()
		if strings.TrimSpace(assistant.Text) == "" && len(assistant.ToolUses) == 0 && len(assistant.Thinking) == 0 {
			return session.Event{}, false, nil
		}
		event.Kind = session.EventAssistantMessage
		event.Assistant = &assistant
		return event, true, nil
	default:
		return session.Event{}, false, nil
	}
}

func CollectAgentToolIDs(events []session.Event) map[string]bool {
	ids := make(map[string]bool)
	for _, event := range events {
		if event.Assistant == nil {
			continue
		}
		for _, tool := range event.Assistant.ToolUses {
			if tool.Name == session.ToolAgent && tool.ID != "" {
				ids[tool.ID] = true
			}
		}
	}
	return ids
}
