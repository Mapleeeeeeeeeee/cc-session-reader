package claudecodec

import (
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

// ADR-009: Claude Code (CLI >= 2.1.165) writes a top-level "promptSource"
// field on some user entries. These cases pin how it is carried onto
// session.UserMessage and how it interacts with the pre-existing text
// classifiers in classify.go.

func userMessageEventFor(t *testing.T, line string) *session.UserMessage {
	t.Helper()
	event, ok, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine returned error: %v", err)
	}
	if !ok {
		t.Fatalf("ParseLine dropped the entry")
	}
	if event.User == nil {
		t.Fatalf("event.User is nil")
	}
	return event.User
}

func TestParseLine_GivenPromptSource_WhenParsed_ThenCarriesItOntoUserMessage(t *testing.T) {
	line := `{"type":"user","timestamp":"2026-09-02T00:00:00Z","message":{"role":"user","content":"為什麼 K 會被高估？"},"promptSource":"typed"}`

	got := userMessageEventFor(t, line)

	if got.PromptSource != session.PromptSourceTyped {
		t.Errorf("PromptSource = %q, want %q", got.PromptSource, session.PromptSourceTyped)
	}
	if got.Text != "為什麼 K 會被高估？" {
		t.Errorf("Text = %q, want the original body", got.Text)
	}
}

func TestParseLine_GivenNoPromptSourceField_WhenParsed_ThenLeavesItEmpty(t *testing.T) {
	line := `{"type":"user","timestamp":"2026-09-02T00:00:00Z","message":{"role":"user","content":"為什麼 K 會被高估？"}}`

	got := userMessageEventFor(t, line)

	if got.PromptSource != "" {
		t.Errorf("PromptSource = %q, want empty (field absent)", got.PromptSource)
	}
}

// ADR-009 decision 3: a promptSource="system" message that the string
// classifier does recognize keeps its existing compact form (the classified
// domain field survives; only PromptSource is added on top of it).
func TestParseLine_GivenSystemPromptSourceOnRecognizedShape_WhenParsed_ThenKeepsTheClassifiedForm(t *testing.T) {
	line := `{"type":"user","timestamp":"2026-09-02T00:00:00Z",` +
		`"message":{"role":"user","content":"7 background agents were stopped by the user: \"工作區\"."},` +
		`"promptSource":"system"}`

	got := userMessageEventFor(t, line)

	if !got.IsAgentsStopped {
		t.Fatalf("IsAgentsStopped = false, want true (classified form preserved)")
	}
	if got.StoppedAgentCount != 7 {
		t.Errorf("StoppedAgentCount = %d, want 7", got.StoppedAgentCount)
	}
	if got.PromptSource != session.PromptSourceSystem {
		t.Errorf("PromptSource = %q, want %q", got.PromptSource, session.PromptSourceSystem)
	}
}

// ADR-009 decision 3: the 15 observed messages a scheduled/looped prompt
// with promptSource="system" but a shape none of the text classifiers
// recognize — only promptSource itself flags these as harness, at the
// render layer (see harness_role_test.go), not by reclassifying the body
// here.
func TestParseLine_GivenSystemPromptSourceOnUnrecognizedShape_WhenParsed_ThenLeavesTextUnclassified(t *testing.T) {
	line := `{"type":"user","timestamp":"2026-09-02T00:00:00Z",` +
		`"message":{"role":"user","content":"Check if background task abc123 finished."},` +
		`"promptSource":"system"}`

	got := userMessageEventFor(t, line)

	if got.IsClassifiedAsHarness() {
		t.Errorf("IsClassifiedAsHarness() = true, want false: no text classifier recognizes this shape")
	}
	if got.Text != "Check if background task abc123 finished." {
		t.Errorf("Text = %q, want the original body kept verbatim", got.Text)
	}
	if got.PromptSource != session.PromptSourceSystem {
		t.Errorf("PromptSource = %q, want %q", got.PromptSource, session.PromptSourceSystem)
	}
}

// ADR-009 decision 4: 0 observed conflicts, but the rule is pinned so a
// future rewording of a harness message can't silently mislabel a message a
// human actually sent.
func TestParseLine_GivenHumanPromptSourceOnHarnessShapedBody_WhenParsed_ThenPromptSourceWins(t *testing.T) {
	line := `{"type":"user","timestamp":"2026-09-02T00:00:00Z",` +
		`"message":{"role":"user","content":"[Request interrupted by user]"},` +
		`"promptSource":"typed"}`

	got := userMessageEventFor(t, line)

	if got.IsInterrupted {
		t.Errorf("IsInterrupted = true, want false: a human promptSource overrules the harness-shaped body")
	}
	if got.Text != "[Request interrupted by user]" {
		t.Errorf("Text = %q, want the original body kept verbatim", got.Text)
	}
	if got.PromptSource != session.PromptSourceTyped {
		t.Errorf("PromptSource = %q, want %q", got.PromptSource, session.PromptSourceTyped)
	}
}

// Regression: v0.1.76 (ADR-009, PR #13) treated "sdk" as a human source, so
// this task-notification's harness shape got overridden and it rendered as
// raw XML under "user (sdk):" instead of the compact form under "harness:".
// Measured on session 5646d1ce: output grew from 217,272 to 234,940 bytes and
// every harness injection in an sdk-driven session lost its compact form.
// "sdk" marks the session's driver, not who authored a given entry — a
// harness injection inherits it from the surrounding session the same way a
// human-typed prompt does, so a recognized harness shape must win over it.
func TestParseLine_GivenSDKPromptSourceOnRecognizedHarnessShape_WhenParsed_ThenKeepsTheClassifiedForm(t *testing.T) {
	line := `{"type":"user","timestamp":"2026-09-02T00:00:00Z",` +
		`"message":{"role":"user","content":"<task-notification>\n<summary>benchmark done</summary>\n</task-notification>"},` +
		`"promptSource":"sdk"}`

	got := userMessageEventFor(t, line)

	if !got.IsTaskNotification {
		t.Fatalf("IsTaskNotification = false, want true: an sdk promptSource must not override a recognized harness shape")
	}
	if got.PromptSource != session.PromptSourceSDK {
		t.Errorf("PromptSource = %q, want %q", got.PromptSource, session.PromptSourceSDK)
	}
}
