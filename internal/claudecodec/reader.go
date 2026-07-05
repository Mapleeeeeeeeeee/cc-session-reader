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

	if raw.Type == "system" && raw.Subtype == "compact_boundary" {
		event.Kind = session.EventCompactBoundary
		return event, true, nil
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
		} else if classified := classifyHarnessUserMessage(text); classified != nil {
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
		for i := range assistant.ToolUses {
			assistant.ToolUses[i].Cwd = raw.Cwd
		}
		event.Kind = session.EventAssistantMessage
		event.Assistant = &assistant
		return event, true, nil
	default:
		return session.Event{}, false, nil
	}
}

// Codec implements session.TranscriptReader for Claude Code JSONL transcripts.
type Codec struct{}

func (Codec) ReadAll(path string) ([]session.Event, error) {
	return ReadAll(path)
}

// commandTagPrefixes are JSONL user message content prefixes that indicate
// tool/command noise rather than real user prompts.
var commandTagPrefixes = []string{
	"<command-name>",
	"<local-command-stdout>",
	"<bash-input>",
	"<bash-stdout>",
	"<bash-stderr>",
	"<local-command-caveat>",
}

// ScanHeader reads up to 20 lines of a JSONL transcript and returns the first
// timestamp and first real user prompt found. Implements session.HeaderScanner.
func (Codec) ScanHeader(path string) (*session.HeaderInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info := &session.HeaderInfo{}
	scanner := bufio.NewScanner(f)
	linesRead := 0
	for scanner.Scan() && linesRead < 20 {
		linesRead++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var h struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Message   *struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &h); err != nil {
			continue
		}

		if info.Timestamp == "" && h.Timestamp != "" {
			info.Timestamp = h.Timestamp
		}

		if info.FirstUserPrompt == "" && h.Message != nil && h.Message.Role == "user" && h.Type == "user" {
			var text string
			if err := json.Unmarshal(h.Message.Content, &text); err == nil {
				if !hasCommandTagPrefix(text) {
					info.FirstUserPrompt = text
				}
			}
		}

		if info.Timestamp != "" && info.FirstUserPrompt != "" {
			break
		}
	}

	return info, nil
}

func hasCommandTagPrefix(s string) bool {
	for _, prefix := range commandTagPrefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
