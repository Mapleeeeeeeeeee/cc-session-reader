package claudecodec

import (
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

// `drift` trusts the handled-block sets in known_shapes.go. These cases tie
// each member of those sets to a parse result that depends on it, so a block
// type the reader stops reading (or starts reading) while its set entry stays
// the same turns a case red.

func Test_BlockTypeSets_Given_AHandledBlockType_When_Parsed_Then_ReaderUsesItAndSetListsIt(t *testing.T) {
	tests := []struct {
		name      string
		blockType string
		isInner   bool
		line      string
		check     func(event session.Event) bool
	}{
		{
			name:      "text in a user message",
			blockType: "text",
			line:      `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"typed words"}]}}`,
			check:     func(e session.Event) bool { return e.User != nil && e.User.Text == "typed words" },
		},
		{
			name:      "thinking in an assistant message",
			blockType: "thinking",
			line:      `{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"pondering"}]}}`,
			check: func(e session.Event) bool {
				return e.Assistant != nil && len(e.Assistant.Thinking) == 1 && e.Assistant.Thinking[0] == "pondering"
			},
		},
		{
			name:      "tool_use in an assistant message",
			blockType: "tool_use",
			line:      `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{}}]}}`,
			check: func(e session.Event) bool {
				return e.Assistant != nil && len(e.Assistant.ToolUses) == 1 && e.Assistant.ToolUses[0].ID == "toolu_1"
			},
		},
		{
			name:      "tool_result in a user message",
			blockType: "tool_result",
			line:      `{"type":"user","toolUseResult":{},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"out"}]}}`,
			check:     func(e session.Event) bool { return e.Tool != nil && e.Tool.ToolUseID == "toolu_1" },
		},
		{
			name:      "server_tool_use in an assistant message",
			blockType: "server_tool_use",
			line:      `{"type":"assistant","message":{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_1","name":"advisor","input":{}}]}}`,
			check: func(e session.Event) bool {
				return e.Assistant != nil && len(e.Assistant.ToolUses) == 1 && e.Assistant.ToolUses[0].ID == "srvtoolu_1"
			},
		},
		{
			name:      "advisor_tool_result in an assistant message",
			blockType: "advisor_tool_result",
			line:      `{"type":"assistant","message":{"role":"assistant","content":[{"type":"advisor_tool_result","tool_use_id":"srvtoolu_1","content":{"type":"advisor_redacted_result","encrypted_content":"x"}}]}}`,
			check:     func(e session.Event) bool { return e.Tool != nil && e.Tool.ToolUseID == "srvtoolu_1" },
		},
		{
			name:      "text inside a tool_result",
			blockType: "text",
			isInner:   true,
			line:      `{"type":"user","toolUseResult":{},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"inner words"}]}]}}`,
			check:     func(e session.Event) bool { return e.Tool != nil && e.Tool.Text == "inner words" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, ok, err := ParseLine([]byte(tt.line))
			if err != nil || !ok {
				t.Fatalf("ParseLine ok=%v err=%v", ok, err)
			}
			if !tt.check(event) {
				t.Errorf("reader did not use the %q block, event: %+v", tt.blockType, event)
			}

			listed := IsHandledContentBlockType(tt.blockType)
			if tt.isInner {
				listed = IsHandledToolResultBlockType(tt.blockType)
			}
			if !listed {
				t.Errorf("%q is read by the reader but not listed as handled", tt.blockType)
			}
		})
	}
}

func Test_SystemSubtypeSet_Given_CompactBoundary_When_Parsed_Then_ReaderUsesItAndSetListsIt(t *testing.T) {
	event, ok, err := ParseLine([]byte(`{"type":"system","subtype":"compact_boundary","timestamp":"2026-10-01T00:00:00Z"}`))
	if err != nil || !ok {
		t.Fatalf("ParseLine ok=%v err=%v", ok, err)
	}
	if event.Kind != session.EventCompactBoundary {
		t.Errorf("Kind = %q, want %q", event.Kind, session.EventCompactBoundary)
	}
	if !IsKnownSystemSubtype("compact_boundary") {
		t.Error("compact_boundary is dispatched by the reader but not listed as known")
	}
}

func Test_RecordTypes_Given_ATypeTheReaderHasARuleFor_When_Asked_Then_ItIsHandled(t *testing.T) {
	tests := []struct {
		recordType string
		want       bool
	}{
		{recordType: "user", want: true},
		{recordType: "assistant", want: true},
		{recordType: "progress", want: true},
		{recordType: "api-request-blob", want: true},
		{recordType: "brand-new-record", want: false},
		{recordType: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.recordType, func(t *testing.T) {
			if got := IsHandledRecordType(tt.recordType); got != tt.want {
				t.Errorf("IsHandledRecordType(%q) = %v, want %v", tt.recordType, got, tt.want)
			}
		})
	}
}
