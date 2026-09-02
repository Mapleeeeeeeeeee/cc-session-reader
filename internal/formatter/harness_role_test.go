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
		// Regression: the disclaimer that wraps the tag in subagent
		// transcripts made classification miss it, so it rendered as a plain
		// "user:" turn with the disclaimer left in the body.
		"a disclaimer-wrapped task notification still keeps its summary": {
			user: session.UserMessage{
				IsTaskNotification: true,
				Text: "[SYSTEM NOTIFICATION - NOT USER INPUT]\nThis is an automated background-task event.\n\n" +
					"<task-notification>\n<summary>benchmark done</summary>\n</task-notification>",
			},
			wantBody: "[benchmark done]",
		},
		"a coordinator message keeps its body under the coordinator marker": {
			user: session.UserMessage{
				IsCoordinatorMessage: true,
				Text:                 "The coordinator sent a message while you were working:\nplease also update the README",
			},
			wantBody: "[coordinator]\nplease also update the README",
		},
		"a continuation prompt collapses to a marker": {
			user:     session.UserMessage{IsContinuePrompt: true, Text: "Continue from where you left off."},
			wantBody: "[continue]",
		},
		"a worker-fork preamble keeps only its directive": {
			user: session.UserMessage{
				IsForkBoilerplate: true,
				Text: "<fork-boilerplate>\nYou are a worker fork. Execute ONE directive, then stop.\n" +
					"</fork-boilerplate>\nFix the failing test.",
			},
			wantBody: "[fork]\nFix the failing test.",
		},
		"a no-visible-output nudge collapses to a marker": {
			user: session.UserMessage{
				IsNoVisibleOutputNudge: true,
				Text:                   "[Your previous response had no visible output. Please continue and produce a user-visible response.]",
			},
			wantBody: "[nudge: no visible output]",
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

// Unlike the other harness subtypes, a mid-turn message's body is genuinely
// human-typed, so it must keep the "user:" label with the harness wrapper
// (opening line and timing explanation) gone from the output.
func TestFormatReadEvents_GivenMidTurnUserMessage_WhenRendered_ThenLabelsItUserWithoutTheWrapper(t *testing.T) {
	events := []session.Event{{
		Kind:      session.EventUserMessage,
		Timestamp: "2026-08-30T13:00:00Z",
		User: &session.UserMessage{
			IsMidTurnUserMessage: true,
			MidTurnUserText:      "直接改 bug 就好",
			Text: "The user sent a new message while you were working:\n直接改 bug 就好\n\n" +
				"This is how Claude Code surfaces messages the user sends mid-turn — within the running " +
				"turn, often alongside the next tool result, rather than as a separate conversation turn. " +
				"Address the message above as you continue this turn.",
		},
	}}

	var out bytes.Buffer
	if err := FormatReadEvents(events, nil, 0, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}

	got := out.String()
	if want := "[13:00:00] user:\n直接改 bug 就好"; !strings.Contains(got, want) {
		t.Errorf("output missing %q\ngot:\n%s", want, got)
	}
	if strings.Contains(got, "sent a new message while you were working") || strings.Contains(got, "surfaces messages") {
		t.Errorf("output still contains the harness wrapper text\ngot:\n%s", got)
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
