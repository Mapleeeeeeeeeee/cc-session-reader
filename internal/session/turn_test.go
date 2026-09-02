package session

import "testing"

// ADR-008: turn counting used to be whatever survived the character-accounting
// branches in ComputeStats, so every `continue` written for one purpose also
// changed K. These cases pin the policy itself: a message counts when it starts
// a unit of agent work, independent of how much of it survives compression.

func TestCountsAsTurn_GivenMessageKind_WhenCounted_ThenFollowsWorkUnitPolicy(t *testing.T) {
	tests := map[string]struct {
		message UserMessage
		want    bool
	}{
		"a typed message is a turn": {
			message: UserMessage{Text: "把這個修好"},
			want:    true,
		},
		"a teammate message is a turn: it drives a full agent response": {
			message: UserMessage{Text: "…", IsTeammateMessage: true},
			want:    true,
		},
		"a task notification is a turn: it wakes the agent (89% drive an API call)": {
			message: UserMessage{Text: "…", IsTaskNotification: true},
			want:    true,
		},
		"a compaction summary is a turn: the session resumes on it (100%)": {
			message: UserMessage{Text: "…", IsCompactionSummary: true},
			want:    true,
		},
		"a stop hook notice is not: it is the tail of the /goal that started the turn": {
			message: UserMessage{Text: "…", IsStopHookGoal: true},
			want:    false,
		},
		"an agents-stopped notice is not": {
			message: UserMessage{Text: "…", IsAgentsStopped: true},
			want:    false,
		},
		"an interruption sentinel is not": {
			message: UserMessage{Text: "…", IsInterrupted: true},
			want:    false,
		},
		"a skill injection is not: it arrives alongside the message that triggered it": {
			message: UserMessage{Text: "…", IsSkillInjection: true},
			want:    false,
		},
		"a command injection is not, for the same reason": {
			message: UserMessage{Text: "…", IsCommandInjection: true},
			want:    false,
		},
		"a system reminder is not": {
			message: UserMessage{Text: "…", IsSystemReminder: true},
			want:    false,
		},
		"a context usage block is not": {
			message: UserMessage{Text: "…", IsContextUsage: true},
			want:    false,
		},
		"command output is not": {
			message: UserMessage{Text: "…", IsCommandNoise: true},
			want:    false,
		},
		"a command invocation marker is not (unchanged by ADR-008)": {
			message: UserMessage{CommandMarker: "[/goal]"},
			want:    false,
		},
		"a coordinator message is a turn: it starts a round of work like a teammate message": {
			message: UserMessage{Text: "…", IsCoordinatorMessage: true},
			want:    true,
		},
		"a continuation prompt is not: it is the tail of an invocation already started elsewhere": {
			message: UserMessage{Text: "Continue from where you left off.", IsContinuePrompt: true},
			want:    false,
		},
		"a worker-fork preamble is a turn: it starts the fork's work": {
			message: UserMessage{Text: "…", IsForkBoilerplate: true},
			want:    true,
		},
		"a no-visible-output nudge is a turn: it asks for a new response, not a report of one": {
			message: UserMessage{Text: "…", IsNoVisibleOutputNudge: true},
			want:    true,
		},
		"a mid-turn user message is not: the harness says it arrives within the running turn": {
			message: UserMessage{Text: "…", IsMidTurnUserMessage: true, MidTurnUserText: "改一下這裡"},
			want:    false,
		},
		"a command marker wins over a teammate flag set on the same message": {
			message: UserMessage{Text: "…", CommandMarker: "[/goal]", IsTeammateMessage: true},
			want:    false,
		},

		// ADR-009: promptSource always wins, regardless of value — measured
		// 89-98% across all five values, including on a command marker, which
		// the pre-ADR-009 policy above says is not a turn.
		"a promptSource is a turn regardless of value: typed": {
			message: UserMessage{Text: "…", PromptSource: PromptSourceTyped},
			want:    true,
		},
		"a promptSource is a turn regardless of value: system": {
			message: UserMessage{Text: "…", PromptSource: PromptSourceSystem},
			want:    true,
		},
		"a promptSource overrides even a command marker, which alone would not count": {
			message: UserMessage{CommandMarker: "[/goal]", PromptSource: PromptSourceTyped},
			want:    true,
		},
		"a promptSource overrides even a kind the policy above says never starts a turn": {
			message: UserMessage{Text: "…", IsInterrupted: true, PromptSource: PromptSourceSystem},
			want:    true,
		},

		// Regression: v0.1.76 (ADR-009, PR #13) let PromptSource=sdk override
		// CountsAsTurn unconditionally, same as every other value. "sdk" marks
		// the session's driver, not who authored a given entry -- a harness
		// injection inherits it from the surrounding sdk-driven session, so it
		// must defer to its own shape's verdict (ADR-008) instead of always
		// counting.
		"a promptSource of sdk with no recognized shape still counts (it is a genuine sdk-driven prompt)": {
			message: UserMessage{Text: "…", PromptSource: PromptSourceSDK},
			want:    true,
		},
		"a promptSource of sdk on a task notification counts, per the shape's own verdict": {
			message: UserMessage{Text: "…", IsTaskNotification: true, PromptSource: PromptSourceSDK},
			want:    true,
		},
		"a promptSource of sdk on an interruption sentinel does not count, per the shape's own verdict": {
			message: UserMessage{Text: "…", IsInterrupted: true, PromptSource: PromptSourceSDK},
			want:    false,
		},
		"a promptSource of sdk on an agents-stopped notice does not count, per the shape's own verdict": {
			message: UserMessage{Text: "…", IsAgentsStopped: true, PromptSource: PromptSourceSDK},
			want:    false,
		},
		"a promptSource of sdk on a stop hook notice does not count, per the shape's own verdict": {
			message: UserMessage{Text: "…", IsStopHookGoal: true, PromptSource: PromptSourceSDK},
			want:    false,
		},
		"a promptSource of sdk on a skill injection does not count, per the shape's own verdict": {
			message: UserMessage{Text: "…", IsSkillInjection: true, PromptSource: PromptSourceSDK},
			want:    false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := tc.message.CountsAsTurn(); got != tc.want {
				t.Errorf("CountsAsTurn() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCompactStopHookGoal_GivenNotice_WhenCompacted_ThenKeepsOnlyTheCondition(t *testing.T) {
	user := &UserMessage{
		IsStopHookGoal: true,
		GoalCondition:  "把資料結構改為用 XML 的形式呈現",
		Text:           "A session-scoped Stop hook is now active with condition: …",
	}

	if got, want := CompactStopHookGoal(user), "[goal] 把資料結構改為用 XML 的形式呈現"; got != want {
		t.Errorf("CompactStopHookGoal() = %q, want %q", got, want)
	}
}

// Without a condition there is nothing to promote, and claiming an empty goal
// would be worse than showing the notice, so the notice survives intact.
func TestCompactStopHookGoal_GivenNoCondition_WhenCompacted_ThenKeepsTheNotice(t *testing.T) {
	user := &UserMessage{IsStopHookGoal: true, Text: "A session-scoped Stop hook is now active"}

	if got := CompactStopHookGoal(user); got != user.Text {
		t.Errorf("CompactStopHookGoal() = %q, want the original notice", got)
	}
}

func TestCompactCoordinatorMessage_GivenNotice_WhenCompacted_ThenStripsTheOpeningLine(t *testing.T) {
	text := "The coordinator sent a message while you were working:\nplease also update the README"

	got := CompactCoordinatorMessage(text)

	if want := "[coordinator]\nplease also update the README"; got != want {
		t.Errorf("CompactCoordinatorMessage() = %q, want %q", got, want)
	}
}

func TestCompactForkBoilerplate_GivenPreambleWithDirective_WhenCompacted_ThenKeepsOnlyTheDirective(t *testing.T) {
	text := "<fork-boilerplate>\nYou are a worker fork. Execute ONE directive, then stop.\n" +
		"</fork-boilerplate>\nFix the failing test."

	got := CompactForkBoilerplate(text)

	if want := "[fork]\nFix the failing test."; got != want {
		t.Errorf("CompactForkBoilerplate() = %q, want %q", got, want)
	}
}

// A fork preamble with no directive after the closing tag still leaves
// something to read: the marker on its own.
func TestCompactForkBoilerplate_GivenPreambleWithNoDirective_WhenCompacted_ThenKeepsOnlyTheMarker(t *testing.T) {
	text := "<fork-boilerplate>\nYou are a worker fork.\n</fork-boilerplate>"

	got := CompactForkBoilerplate(text)

	if want := "[fork]"; got != want {
		t.Errorf("CompactForkBoilerplate() = %q, want %q", got, want)
	}
}

func TestCompactAgentsStopped_GivenNotice_WhenCompacted_ThenKeepsOnlyTheCount(t *testing.T) {
	user := &UserMessage{IsAgentsStopped: true, StoppedAgentCount: 7}

	if got, want := CompactAgentsStopped(user), "[agents stopped: 7]"; got != want {
		t.Errorf("CompactAgentsStopped() = %q, want %q", got, want)
	}
}

func TestCompactCompactionSummary_GivenInjectedSummary_WhenCompacted_ThenDropsFramingAndKeepsBody(t *testing.T) {
	text := "This session is being continued from a previous conversation that ran " +
		"out of context. The summary below covers the earlier portion of the " +
		"conversation.\n\nSummary:\n1. Primary Request and Intent:\n   蓋 benchmark"

	got := CompactCompactionSummary(text)

	want := "[compaction summary]\nSummary:\n1. Primary Request and Intent:\n   蓋 benchmark"
	if got != want {
		t.Errorf("CompactCompactionSummary() = %q, want %q", got, want)
	}
}

// A summary whose body never reaches the "Summary:" heading must not be
// silently emptied — the body is the previous conversation.
func TestCompactCompactionSummary_GivenNoSummaryHeading_WhenCompacted_ThenKeepsWholeBody(t *testing.T) {
	text := "This session is being continued from a previous conversation.\n\n之前在查 K"

	got := CompactCompactionSummary(text)

	if got != "[compaction summary]\n"+text {
		t.Errorf("CompactCompactionSummary() = %q, want the whole body kept", got)
	}
}
