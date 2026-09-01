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

// The first disclaimer wording matched 0 of the 133 teammate messages observed
// in the 60 days before 2026-08-30 — the harness had reworded it, and detection
// survived only because the opening line happened to match every one. Each
// marker must classify on its own so a single rewording cannot break detection.
func TestClassifyHarnessUserMessage_GivenTeammateMarkerVariant_WhenClassified_ThenStillDetectsIt(t *testing.T) {
	const block = "<teammate-message teammate_id=\"rebase-389\" summary=\"done\">\nRebase 完成。\n</teammate-message>"

	tests := map[string]string{
		"the opening line alone": "Another Claude session sent a message:\n" + block,
		"the current disclaimer alone": block +
			"\n\nThis came from another Claude session — not typed by your user.",
		"the superseded disclaimer alone": block +
			"\n\nIMPORTANT: This is NOT from your user, but from another Claude session.",
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
	}

	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			if got := classifyHarnessUserMessage(text); got != nil {
				t.Errorf("classifyHarnessUserMessage(%q) = %+v, want nil", text, got)
			}
		})
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
