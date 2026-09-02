package claudecodec

import (
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

// ADR-008: these six shapes reached the formatter unclassified, so they were
// rendered verbatim under a "user" label and counted toward K. The bodies here
// are the real harness wording observed in transcripts from the 60 days before
// 2026-08-30.

func TestClassifyHarnessUserMessage_GivenHarnessInjection_WhenClassified_ThenSetsItsDomainField(t *testing.T) {
	tests := map[string]struct {
		text  string
		check func(*testing.T, *session.UserMessage)
	}{
		"a background-task report is a task notification": {
			text: "<task-notification>\n<task-id>bxw75arip</task-id>\n" +
				"<summary>Background command \"Run both benchmarks\" completed (exit code 0)</summary>\n" +
				"</task-notification>",
			check: func(t *testing.T, got *session.UserMessage) {
				if !got.IsTaskNotification {
					t.Error("IsTaskNotification = false, want true")
				}
			},
		},
		"a continuation prompt is a compaction summary": {
			text: "This session is being continued from a previous conversation that ran " +
				"out of context.\n\nSummary:\n1. Primary Request and Intent:",
			check: func(t *testing.T, got *session.UserMessage) {
				if !got.IsCompactionSummary {
					t.Error("IsCompactionSummary = false, want true")
				}
			},
		},
		"an interruption sentinel is marked interrupted": {
			text: "[Request interrupted by user]",
			check: func(t *testing.T, got *session.UserMessage) {
				if !got.IsInterrupted {
					t.Error("IsInterrupted = false, want true")
				}
			},
		},
		"the tool-use variant of the sentinel is too": {
			text: "[Request interrupted by user for tool use]",
			check: func(t *testing.T, got *session.UserMessage) {
				if !got.IsInterrupted {
					t.Error("IsInterrupted = false, want true")
				}
			},
		},
		"an agents-stopped notice carries the count": {
			text: `7 background agents were stopped by the user: "工作區：` +
				"`/Users/maple/Desktop/nccu-toolkit/.claude/wor...\".",
			check: func(t *testing.T, got *session.UserMessage) {
				if !got.IsAgentsStopped {
					t.Fatal("IsAgentsStopped = false, want true")
				}
				if got.StoppedAgentCount != 7 {
					t.Errorf("StoppedAgentCount = %d, want 7", got.StoppedAgentCount)
				}
			},
		},
		"a stop hook notice carries the goal condition": {
			text: `A session-scoped Stop hook is now active with condition: "開一個 branch ` +
				`測試，把資料結構改為用 XML 的形式呈現". Briefly acknowledge the goal, then ` +
				"immediately start working toward it.",
			check: func(t *testing.T, got *session.UserMessage) {
				if !got.IsStopHookGoal {
					t.Fatal("IsStopHookGoal = false, want true")
				}
				want := "開一個 branch 測試，把資料結構改為用 XML 的形式呈現"
				if got.GoalCondition != want {
					t.Errorf("GoalCondition = %q, want %q", got.GoalCondition, want)
				}
			},
		},
		"a skill re-invocation notice reuses the skill-injection path": {
			text: "Skill /artifact-design was loaded earlier (see the invoked-skills " +
				"reminder above); this is a NEW invocation — follow those instructions now.",
			check: func(t *testing.T, got *session.UserMessage) {
				if !got.IsSkillInjection {
					t.Fatal("IsSkillInjection = false, want true")
				}
				if got.SkillName != "artifact-design" {
					t.Errorf("SkillName = %q, want %q", got.SkillName, "artifact-design")
				}
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := classifyHarnessUserMessage(tc.text)
			if got == nil {
				t.Fatalf("classifyHarnessUserMessage(%q) = nil, want a classified message", tc.text)
			}
			tc.check(t, got)
		})
	}
}

// Detection anchors on the XML tag alone, not the surrounding prose: the
// disclaimer wording has already changed once (0 of 133 teammate messages
// matched the superseded form in the 60 days before 2026-08-30), and 80 of
// 1,112 teammate messages in that window used the <agent-message> tag with
// neither the opening line nor a disclaimer at all.
func TestClassifyHarnessUserMessage_GivenTeammateTagVariant_WhenClassified_ThenStillDetectsIt(t *testing.T) {
	const teammateBlock = "<teammate-message teammate_id=\"rebase-389\" summary=\"done\">\nRebase 完成。\n</teammate-message>"
	const agentBlock = "<agent-message from=\"cdcv13-finish\">\n工作完成。\n</agent-message>"

	tests := map[string]string{
		"the opening line alone": "Another Claude session sent a message:\n" + teammateBlock,
		"the current disclaimer alone": teammateBlock +
			"\n\nThis came from another Claude session — not typed by your user.",
		"the superseded disclaimer alone": teammateBlock +
			"\n\nIMPORTANT: This is NOT from your user, but from another Claude session.",
		// Regression: <agent-message> carried none of the above and was
		// rendered verbatim as a plain user turn (ADR-008 "沒有解決的").
		"the agent-message variant, tag only, no opening line or disclaimer": agentBlock,
		"the agent-message variant with no attributes at all":                "<agent-message>\n工作完成。\n</agent-message>",
	}

	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			got := classifyHarnessUserMessage(text)
			if got == nil || !got.IsTeammateMessage {
				t.Errorf("classifyHarnessUserMessage() did not classify a teammate message: %+v", got)
			}
		})
	}
}

// A message that merely quotes harness wording mid-body is a real user message.
func TestClassifyHarnessUserMessage_GivenPlainMessage_WhenClassified_ThenReturnsNil(t *testing.T) {
	tests := map[string]string{
		"a typed question":                      "為什麼 K 會被高估？",
		"a quoted notice inside a real message": "我看到 [Request interrupted by user] 之後就沒反應了，為什麼？",
		// Detection anchors on the tag, not the opening line: mentioning the
		// harness prose without the tag must not misclassify a real message.
		"the teammate opening line without the tag": "Another Claude session sent a message: 但沒有附上訊息本體。",
	}

	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			if got := classifyHarnessUserMessage(text); got != nil {
				t.Errorf("classifyHarnessUserMessage(%q) = %+v, want nil", text, got)
			}
		})
	}
}

// The disclaimer wraps the tag in a CLI build seen only in subagent
// transcripts (68 messages, CLI 2.1.200-2.1.235); HasPrefix missed all of
// them since the tag no longer opens the message.
func TestClassifyHarnessUserMessage_GivenTaskNotificationWrappedInDisclaimer_WhenClassified_ThenStillDetectsIt(t *testing.T) {
	text := "[SYSTEM NOTIFICATION - NOT USER INPUT]\n" +
		"This is an automated background-task event, NOT a message from the user.\n" +
		"Do NOT interpret this as user acknowledgement, confirmation, or response to any pending question.\n\n" +
		"<task-notification>\n<task-id>a3d084a486cbf8046</task-id>\n" +
		"<summary>Build finished</summary>\n</task-notification>"

	got := classifyHarnessUserMessage(text)

	if got == nil || !got.IsTaskNotification {
		t.Fatalf("classifyHarnessUserMessage() = %+v, want IsTaskNotification = true", got)
	}
}

// 3 messages in subagent transcripts: the coordinator opens a round of work
// the same way a teammate message does, but with different framing.
func TestClassifyHarnessUserMessage_GivenCoordinatorMessage_WhenClassified_ThenMarksIt(t *testing.T) {
	text := "The coordinator sent a message while you were working:\nplease also update the README"

	got := classifyHarnessUserMessage(text)

	if got == nil || !got.IsCoordinatorMessage {
		t.Fatalf("classifyHarnessUserMessage() = %+v, want IsCoordinatorMessage = true", got)
	}
}

// classifyContinuePrompt requires the isMeta flag in addition to the exact
// text, since isMeta alone also covers image placeholders and stop-hook
// feedback (see classifySkillInjectionByLink).
func TestClassifyContinuePrompt_GivenExactTextAndIsMeta_WhenClassified_ThenMarksIt(t *testing.T) {
	tests := map[string]struct {
		text   string
		isMeta bool
		want   bool
	}{
		"exact text with isMeta true is a continue prompt": {
			text: "Continue from where you left off.", isMeta: true, want: true,
		},
		"exact text without isMeta is not, since isMeta alone is not distinctive": {
			text: "Continue from where you left off.", isMeta: false, want: false,
		},
		"isMeta true with different text is not": {
			text: "Continue from where you left off, please.", isMeta: true, want: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := classifyContinuePrompt(tc.text, tc.isMeta)
			if (got != nil) != tc.want {
				t.Errorf("classifyContinuePrompt(%q, %v) = %+v, want non-nil = %v", tc.text, tc.isMeta, got, tc.want)
			}
			if tc.want && !got.IsContinuePrompt {
				t.Errorf("IsContinuePrompt = false, want true")
			}
		})
	}
}

// 5 messages, all subagent transcripts: the worker-fork preamble is fixed
// boilerplate, and any text after the closing tag is the fork's directive.
func TestClassifyHarnessUserMessage_GivenForkBoilerplate_WhenClassified_ThenMarksIt(t *testing.T) {
	text := "<fork-boilerplate>\nYou are a worker fork. The transcript above is the parent's " +
		"history. Execute ONE directive, then stop.\n</fork-boilerplate>\nFix the failing test."

	got := classifyHarnessUserMessage(text)

	if got == nil || !got.IsForkBoilerplate {
		t.Fatalf("classifyHarnessUserMessage() = %+v, want IsForkBoilerplate = true", got)
	}
}

// 36 messages, exact text, main sessions: the harness's nudge to produce a
// visible response after a silent turn.
func TestClassifyHarnessUserMessage_GivenNoVisibleOutputNudge_WhenClassified_ThenMarksIt(t *testing.T) {
	text := "[Your previous response had no visible output. Please continue and produce a user-visible response.]"

	got := classifyHarnessUserMessage(text)

	if got == nil || !got.IsNoVisibleOutputNudge {
		t.Fatalf("classifyHarnessUserMessage() = %+v, want IsNoVisibleOutputNudge = true", got)
	}
}

// 35 messages: the body is human-typed, so classification must keep it under
// the user role (not harness) while still stripping the harness's wrapper.
func TestClassifyHarnessUserMessage_GivenMidTurnUserMessage_WhenClassified_ThenExtractsTheUserText(t *testing.T) {
	text := "The user sent a new message while you were working:\n直接改 bug 就好\n\n" +
		"This is how Claude Code surfaces messages the user sends mid-turn — within the running " +
		"turn, often alongside the next tool result, rather than as a separate conversation turn. " +
		"Address the message above as you continue this turn."

	got := classifyHarnessUserMessage(text)

	if got == nil || !got.IsMidTurnUserMessage {
		t.Fatalf("classifyHarnessUserMessage() = %+v, want IsMidTurnUserMessage = true", got)
	}
	if want := "直接改 bug 就好"; got.MidTurnUserText != want {
		t.Errorf("MidTurnUserText = %q, want %q", got.MidTurnUserText, want)
	}
}

// 2 messages render as "user:" because the stderr variant of the
// local-command tag has no handling, unlike its stdout sibling.
func TestClassifyCommandUserMessage_GivenLocalCommandStderr_WhenClassified_ThenMarksItCommandNoise(t *testing.T) {
	text := "<local-command-stderr>permission denied</local-command-stderr>"

	got := classifyCommandUserMessage(text)

	if got == nil || !got.IsCommandNoise {
		t.Fatalf("classifyCommandUserMessage(%q) = %+v, want IsCommandNoise = true", text, got)
	}
}

// These entry types were added to Claude Code after the original noise list.
// Without an entry they fell through as unparsed rather than as EventNoise,
// which left their bytes out of the analyzer's system_noise accounting.
func TestParseLine_GivenRecentlyAddedEntryType_WhenParsed_ThenYieldsNoise(t *testing.T) {
	types := []string{
		"atis-latch", "frame-link", "worktree-state", "file-history-delta",
		"artifact-autoreact-ledger", "artifact-comment-monitor",
		"agent-setting", "cost-state",
		// Found when the ADR-008 scan was extended to the subagent
		// transcript layer: 600 entries / 82 KB in the same 60-day window.
		"relocated",
	}

	for _, entryType := range types {
		t.Run(entryType, func(t *testing.T) {
			line := []byte(`{"type":"` + entryType + `","sessionId":"abc","timestamp":"2026-08-30T13:00:00Z"}`)

			event, ok, err := ParseLine(line)
			if err != nil {
				t.Fatalf("ParseLine returned error: %v", err)
			}
			if !ok {
				t.Fatalf("ParseLine dropped %q instead of yielding noise", entryType)
			}
			if event.Kind != session.EventNoise {
				t.Errorf("Kind = %q, want %q", event.Kind, session.EventNoise)
			}
		})
	}
}
