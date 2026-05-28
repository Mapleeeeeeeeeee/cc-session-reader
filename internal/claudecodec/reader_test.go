package claudecodec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func TestParseLine_UserStringContent(t *testing.T) {
	event := parseLine(t, `{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":"hello"}}`)
	if event.Kind != session.EventUserMessage {
		t.Fatalf("kind = %s, want %s", event.Kind, session.EventUserMessage)
	}
	if event.User == nil || event.User.Text != "hello" {
		t.Fatalf("user text = %#v, want hello", event.User)
	}
}

func TestParseLine_UserUnknownContentShapeKeepsRawJSON(t *testing.T) {
	event := parseLine(t, `{"type":"user","timestamp":"2026-05-28T00:00:00Z","message":{"role":"user","content":{"unexpected":"shape"}}}`)
	if event.Kind != session.EventUserMessage {
		t.Fatalf("kind = %s, want %s", event.Kind, session.EventUserMessage)
	}
	if event.User == nil || event.User.Text != `{"unexpected":"shape"}` {
		t.Fatalf("user text = %#v, want raw JSON", event.User)
	}
}

func TestParseLine_AssistantBlocks(t *testing.T) {
	event := parseLine(t, `{"type":"assistant","timestamp":"2026-05-28T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hi"},{"type":"thinking","thinking":"private reasoning"},{"type":"tool_use","name":"Bash","id":"tool-1","input":{"command":"echo ok","description":"Echo ok"}}]}}`)
	if event.Kind != session.EventAssistantMessage {
		t.Fatalf("kind = %s, want %s", event.Kind, session.EventAssistantMessage)
	}
	if event.Assistant == nil {
		t.Fatal("assistant is nil")
	}
	if event.Assistant.Text != "hi" {
		t.Fatalf("assistant text = %q, want hi", event.Assistant.Text)
	}
	if got := event.Assistant.Thinking; len(got) != 1 || got[0] != "private reasoning" {
		t.Fatalf("thinking = %#v", got)
	}
	if got := event.Assistant.ToolUses; len(got) != 1 || got[0].Name != "Bash" || got[0].ID != "tool-1" || got[0].Input.String("description") != "Echo ok" {
		t.Fatalf("tool uses = %#v", got)
	}
}

func TestParseLine_ToolResultStringContent(t *testing.T) {
	event := parseLine(t, `{"type":"user","timestamp":"2026-05-28T00:00:02Z","toolUseResult":{"success":true,"commandName":"Bash"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}]}}`)
	if event.Kind != session.EventToolResult {
		t.Fatalf("kind = %s, want %s", event.Kind, session.EventToolResult)
	}
	if event.Tool == nil || event.Tool.ToolUseID != "tool-1" || event.Tool.Text != "ok" || event.Tool.RawName != "Bash" || !event.Tool.Success {
		t.Fatalf("tool result = %#v", event.Tool)
	}
}

func TestParseLine_ToolResultTextBlockContent(t *testing.T) {
	event := parseLine(t, `{"type":"user","toolUseResult":{"success":false,"commandName":"Read"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-2","content":[{"type":"text","text":"first part"},{"type":"text","text":"second part"}]}]}}`)
	if event.Tool == nil || event.Tool.Text != "first part\nsecond part" || event.Tool.Success {
		t.Fatalf("tool result = %#v", event.Tool)
	}
}

func TestParseLine_UserAnswer(t *testing.T) {
	event := parseLine(t, `{"type":"user","toolUseResult":{"success":true},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-3","content":"User has answered your questions: ship it"}]}}`)
	if event.User == nil || !event.User.IsAnswer || event.User.Text != "User has answered your questions: ship it" {
		t.Fatalf("user answer = %#v", event.User)
	}
}

func TestParseLine_Noise(t *testing.T) {
	event := parseLine(t, `{"type":"system","message":{"content":"system details"}}`)
	if event.Kind != session.EventNoise {
		t.Fatalf("kind = %s, want %s", event.Kind, session.EventNoise)
	}
	if event.Noise == nil || event.Noise.Text != "system details" {
		t.Fatalf("noise = %#v", event.Noise)
	}
}

func TestParseLine_UnknownEntryWithoutMessageIsSkipped(t *testing.T) {
	_, ok, err := ParseLine([]byte(`{"type":"future-event","payload":{"value":1}}`))
	if err != nil {
		t.Fatalf("ParseLine returned error: %v", err)
	}
	if ok {
		t.Fatal("unknown entry without message should be skipped")
	}
}

func TestReadFile_WhenLineIsMalformed_ThenReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.jsonl")
	data := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"ok"}}`,
		`{"type":"user","message":`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	err := ReadFile(path, func(session.Event) error { return nil })
	if err == nil {
		t.Fatal("ReadFile returned nil, want malformed JSON error")
	}
	if !strings.Contains(err.Error(), "parse transcript line") {
		t.Fatalf("error = %v, want parse transcript line", err)
	}
}

func TestReadFile_WhenLastLineHasNoTrailingNewline_ThenReadsEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := `{"type":"user","message":{"role":"user","content":"ok"}}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var events []session.Event
	err := ReadFile(path, func(event session.Event) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if len(events) != 1 || events[0].User == nil || events[0].User.Text != "ok" {
		t.Fatalf("events = %#v, want one user event", events)
	}
}

func TestCollectAgentToolIDs(t *testing.T) {
	events := []session.Event{
		{
			Kind: session.EventAssistantMessage,
			Assistant: &session.AssistantMessage{ToolUses: []session.ToolUse{
				{ID: "agent-1", Name: "Agent"},
				{ID: "bash-1", Name: "Bash"},
			}},
		},
	}
	ids := CollectAgentToolIDs(events)
	if !ids["agent-1"] || ids["bash-1"] {
		t.Fatalf("ids = %#v", ids)
	}
}

func TestParseLine_AllNoiseTypes(t *testing.T) {
	noiseTypes := []string{
		"file-history-snapshot", "attachment", "bridge-session",
		"last-prompt", "permission-mode", "mode", "ai-title",
		"custom-title", "agent-name", "pr-link",
		"queue-operation", "progress", "system",
	}
	for _, typ := range noiseTypes {
		t.Run(typ, func(t *testing.T) {
			line := fmt.Sprintf(`{"type":"%s","message":{"role":"user","content":"x"}}`, typ)
			event, ok, err := ParseLine([]byte(line))
			if err != nil {
				t.Fatalf("ParseLine(%s) error: %v", typ, err)
			}
			if !ok {
				t.Fatalf("ParseLine(%s) returned ok=false, want ok=true with noise event", typ)
			}
			if event.Kind != session.EventNoise {
				t.Fatalf("ParseLine(%s) kind = %s, want %s", typ, event.Kind, session.EventNoise)
			}
		})
	}
}

func parseLine(t *testing.T, line string) session.Event {
	t.Helper()
	event, ok, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine returned error: %v", err)
	}
	if !ok {
		t.Fatal("ParseLine skipped line")
	}
	return event
}
