package formatter

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

// ADR-008: harness-injected messages were labelled "user", identical to a
// message the person actually typed, so a reader inheriting the transcript
// could not tell them apart.

func TestFormatReadEvents_GivenHarnessInjection_WhenRendered_ThenLabelsItHarness(t *testing.T) {
	tests := map[string]struct {
		user     session.UserMessage
		wantBody string
	}{
		"a stop hook notice keeps only its goal": {
			user: session.UserMessage{
				IsStopHookGoal: true,
				GoalCondition:  "把資料結構改為用 XML 的形式呈現",
				Text:           "A session-scoped Stop hook is now active with condition: …",
			},
			wantBody: "[goal] 把資料結構改為用 XML 的形式呈現",
		},
		"an agents-stopped notice keeps only its count": {
			user:     session.UserMessage{IsAgentsStopped: true, StoppedAgentCount: 7, Text: "7 background agents…"},
			wantBody: "[agents stopped: 7]",
		},
		"an interruption sentinel collapses to a marker": {
			user:     session.UserMessage{IsInterrupted: true, Text: "[Request interrupted by user]"},
			wantBody: "[interrupted]",
		},
		"a task notification keeps its summary": {
			user: session.UserMessage{
				IsTaskNotification: true,
				Text:               "<task-notification>\n<summary>benchmark done</summary>\n</task-notification>",
			},
			wantBody: "[benchmark done]",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			events := []session.Event{{
				Kind:      session.EventUserMessage,
				Timestamp: "2026-08-30T13:00:00Z",
				User:      &tc.user,
			}}

			var out bytes.Buffer
			if err := FormatReadEvents(events, nil, 0, 0, FormatOptions{}, &out); err != nil {
				t.Fatalf("FormatReadEvents returned error: %v", err)
			}

			want := "[13:00:00] harness:\n" + tc.wantBody
			if !strings.Contains(out.String(), want) {
				t.Errorf("output missing %q\ngot:\n%s", want, out.String())
			}
		})
	}
}

func TestFormatReadEvents_GivenTypedMessage_WhenRendered_ThenStillLabelsItUser(t *testing.T) {
	events := []session.Event{
		{
			Kind:      session.EventUserMessage,
			Timestamp: "2026-08-30T13:00:00Z",
			User:      &session.UserMessage{CommandMarker: "[/goal]"},
		},
		{
			Kind:      session.EventUserMessage,
			Timestamp: "2026-08-30T13:00:01Z",
			User:      &session.UserMessage{Text: "為什麼 K 會被高估？"},
		},
	}

	var out bytes.Buffer
	if err := FormatReadEvents(events, nil, 0, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}

	got := out.String()
	for _, want := range []string{"[13:00:00] user:\n[/goal]", "[13:00:01] user:\n為什麼 K 會被高估？"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, got)
		}
	}
}
