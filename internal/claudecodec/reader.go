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

	// toolNames accumulates tool_use_id -> tool name across the sequential
	// read, scoped to this file. See parseLineWithToolNames for why this
	// state can't live inside the stateless public ParseLine.
	toolNames := map[string]string{}
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
		event, ok, parseErr := parseLineWithToolNames(line, toolNames)
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
	return parseLineWithToolNames(line, nil)
}

// parseLineWithToolNames is ParseLine's implementation, extended with the
// tool_use_id -> tool name state ReadFile accumulates across a sequential
// read. Real transcripts carry no commandName/agentType field on
// Bash/Edit/Write/Read toolUseResults — the tool name only exists on the
// preceding assistant tool_use block, correlated by tool_use_id — so a
// tool_result's name/DiffStat can only be resolved with that cross-line
// state. ParseLine keeps its public, stateless single-line contract by
// passing toolNames=nil, under which name resolution falls back to
// commandName/agentType only, same as before this fix.
func parseLineWithToolNames(line []byte, toolNames map[string]string) (session.Event, bool, error) {
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
		toolResult := raw.toToolResult(toolNames)
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
		if toolNames != nil {
			for _, toolUse := range assistant.ToolUses {
				toolNames[toolUse.ID] = toolUse.Name
			}
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

// headerScanCandidateBudget bounds how many genuine (non-noise) user-authored
// lines ScanHeader inspects before giving up on finding a real prompt.
// Known-noise shapes — command invocations/output, skill injections, tool
// results, empty text — don't consume this budget: a session that opens with
// a long run of harness noise before the first real question must not
// exhaust the budget on noise alone (see classifyCommandUserMessage /
// classifyHarnessUserMessage below for what counts as noise).
const headerScanCandidateBudget = 20

// headerScanMaxPhysicalLines hard-caps the raw lines ScanHeader reads,
// independent of headerScanCandidateBudget. Because noise no longer consumes
// the candidate budget, a session with no real user message at all (e.g. a
// fully automated inherit chain) needs this separate bound so the scan still
// terminates instead of reading toward EOF.
const headerScanMaxPhysicalLines = 200

// commandPreviewMaxRunes caps the synthesized command-fallback preview (see
// commandFallbackPreview) at the same width as the bang-command marker.
const commandPreviewMaxRunes = 80

// requestInterruptedPrefix marks the fixed harness sentinel Claude Code
// inserts as a user-role text block when a tool call is interrupted (seen as
// both "[Request interrupted by user]" and "[Request interrupted by user for
// tool use]" across real transcripts). It is structurally indistinguishable
// from a real question — same shape, same role — but it is never something
// the human typed, so ScanHeader must not pick it as the list preview ahead
// of an actual question or the command-fallback marker.
const requestInterruptedPrefix = "[Request interrupted by user"

// headerLine is the minimal shape ScanHeader needs from a transcript line.
// Message reuses rawMessage so command/skill-injection classification (and
// its text normalization across string vs. block-array content) stays a
// single source of truth shared with the full parse path in classify.go.
type headerLine struct {
	Type      string      `json:"type"`
	Timestamp string      `json:"timestamp"`
	Message   *rawMessage `json:"message"`
}

// ScanHeader reads a bounded prefix of a JSONL transcript and returns its
// first timestamp, its last timestamp (from a bounded tail read — see
// readLastTimestamp — not a full scan), and the first real user prompt
// found. Implements session.HeaderScanner.
//
// Known-noise user-role lines (command invocations/output, skill injections,
// tool results, empty text, interrupted-request sentinels) are skipped
// without consuming the scan budget. If no real prompt turns up within that
// budget, FirstUserPrompt falls back
// to a one-line preview of the first command invocation seen, e.g.
// "[/cc-session inherit 9cd01951]" — enough to identify the session in an
// inherit chain even when the real question lies far beyond the scan window.
func (Codec) ScanHeader(path string) (*session.HeaderInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info := &session.HeaderInfo{}
	var commandFallback string
	candidateBudget := headerScanCandidateBudget

	scanner := bufio.NewScanner(f)
	physicalLines := 0
	for scanner.Scan() && physicalLines < headerScanMaxPhysicalLines {
		physicalLines++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var h headerLine
		if err := json.Unmarshal(line, &h); err != nil {
			continue
		}

		if info.Timestamp == "" && h.Timestamp != "" {
			info.Timestamp = h.Timestamp
		}

		if h.Type != "user" || h.Message == nil || h.Message.Role != "user" {
			continue
		}

		text := h.Message.Text()

		if classified := classifyCommandUserMessage(text); classified != nil {
			captureCommandFallback(&commandFallback, text)
			continue
		}
		if classified := classifyHarnessUserMessage(text); classified != nil {
			captureCommandFallback(&commandFallback, text)
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || strings.HasPrefix(trimmed, requestInterruptedPrefix) {
			continue
		}

		if info.FirstUserPrompt == "" {
			info.FirstUserPrompt = text
		}
		candidateBudget--
		if candidateBudget <= 0 || (info.Timestamp != "" && info.FirstUserPrompt != "") {
			break
		}
	}

	if info.FirstUserPrompt == "" && commandFallback != "" {
		info.FirstUserPrompt = commandFallback
	}

	if ts, err := readLastTimestamp(f); err == nil && ts != "" {
		info.EndTimestamp = ts
	}

	return info, nil
}

// captureCommandFallback records the first command invocation's one-line
// preview into *fallback. It is a no-op once a preview has already been
// captured, and for lines that carry no <command-name> tag at all (e.g.
// skill injections, which reach this call too since both known-noise
// branches in ScanHeader route through it).
func captureCommandFallback(fallback *string, text string) {
	if *fallback != "" {
		return
	}
	if preview := commandFallbackPreview(text); preview != "" {
		*fallback = preview
	}
}

// commandFallbackPreview extracts the <command-name>/<command-args> pair from
// a command invocation entry and renders it as a compact marker, e.g.
// "[/cc-session inherit 9cd01951]". Returns "" when text carries no
// <command-name> tag (the caller then leaves the fallback unset).
func commandFallbackPreview(text string) string {
	name := strings.TrimSpace(extractBetween(text, tagCommandNameOpen, tagCommandNameClose))
	if name == "" {
		return ""
	}
	marker := name
	if args := strings.TrimSpace(extractBetween(text, tagCommandArgsOpen, tagCommandArgsClose)); args != "" {
		marker += " " + shortenCommandArgs(args)
	}
	return "[" + session.Truncate(marker, commandPreviewMaxRunes) + "]"
}

// shortenCommandArgs truncates each whitespace-separated argument token to
// session.ShortID's 8-rune convention, so a UUID session-ID argument reads as
// its identifying prefix instead of the full 36-character value.
func shortenCommandArgs(args string) string {
	fields := strings.Fields(args)
	for i, field := range fields {
		fields[i] = session.ShortID(field, 8)
	}
	return strings.Join(fields, " ")
}
