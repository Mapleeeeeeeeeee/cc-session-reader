package claudecodec

import (
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func Test_ParseLine_Given_AdvisorToolResultContent_When_Parsed_Then_MapsToStatusAndText(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantSuccess bool
		wantText    string
	}{
		{
			name:        "encrypted reply",
			content:     `{"type":"advisor_redacted_result","encrypted_content":"AAAA"}`,
			wantSuccess: true,
			wantText:    "response is encrypted in the transcript",
		},
		{
			name:        "error reply",
			content:     `{"type":"advisor_tool_result_error","error_code":"too_many_requests"}`,
			wantSuccess: false,
			wantText:    "too_many_requests",
		},
		{
			name:        "content type without a rule keeps its raw JSON",
			content:     `{"text":"use a queue","type":"advisor_result"}`,
			wantSuccess: true,
			wantText:    `{"text":"use a queue","type":"advisor_result"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"advisor_tool_result","tool_use_id":"srvtoolu_1","content":` + tt.content + `}]}}`
			event, ok, err := ParseLine([]byte(line))
			if err != nil || !ok {
				t.Fatalf("ParseLine ok=%v err=%v", ok, err)
			}
			if event.Kind != session.EventToolResult || event.Tool == nil {
				t.Fatalf("event = %+v, want a tool result", event)
			}
			if event.Tool.ToolUseID != "srvtoolu_1" || event.Tool.RawName != session.ToolAdvisor {
				t.Errorf("ToolUseID/RawName = %q/%q, want srvtoolu_1/%s", event.Tool.ToolUseID, event.Tool.RawName, session.ToolAdvisor)
			}
			if event.Tool.Success != tt.wantSuccess || event.Tool.Text != tt.wantText {
				t.Errorf("Success/Text = %v/%q, want %v/%q", event.Tool.Success, event.Tool.Text, tt.wantSuccess, tt.wantText)
			}
		})
	}
}
