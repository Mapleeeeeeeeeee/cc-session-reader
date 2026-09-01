package claudecodec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression: bundled skills such as artifact-design inject their body with
// no "Base directory for this skill:" line — the text starts directly with
// the skill's own prose — so classifyHarnessUserMessage's text-prefix path
// never classified them, and the injection rendered as a full "user:"
// message. The harness instead links the injection to its Skill tool_use via
// isMeta/sourceToolUseID, which ReadFile can resolve across lines.
func TestReadAll_GivenSkillInjectionWithoutBaseDirectoryLine_ThenClassifiedViaSourceToolUseLink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"assistant","timestamp":"2026-08-30T00:00:00Z","message":{"role":"assistant","content":[` +
			`{"type":"tool_use","id":"toolu_skill1","name":"Skill","input":{"skill":"artifact-design"}}` +
			`]}}`,
		`{"type":"user","timestamp":"2026-08-30T00:00:01Z","isMeta":true,"sourceToolUseID":"toolu_skill1",` +
			`"message":{"role":"user","content":"Approach this as the design lead at a small studio..."}}`,
		"",
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	events, err := ReadAll(path)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if len(events) != 2 || events[1].User == nil {
		t.Fatalf("events = %#v, want an assistant event followed by a user event", events)
	}

	user := events[1].User
	if !user.IsSkillInjection {
		t.Fatalf("IsSkillInjection = false, want true (linked via sourceToolUseID)")
	}
	if user.SkillName != "artifact-design" {
		t.Errorf("SkillName = %q, want %q", user.SkillName, "artifact-design")
	}
}

// A skill invoked with ARGUMENTS carries them on the Skill tool_use's "args"
// input, not in the injected body text, so the link path must read args from
// there rather than leaving SkillArgs empty.
func TestReadAll_GivenSkillInjectionWithArgsViaLink_ThenSkillArgsPopulated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"assistant","timestamp":"2026-08-30T00:00:00Z","message":{"role":"assistant","content":[` +
			`{"type":"tool_use","id":"toolu_skill2","name":"Skill","input":{"skill":"pm","args":"build login page"}}` +
			`]}}`,
		`{"type":"user","timestamp":"2026-08-30T00:00:01Z","isMeta":true,"sourceToolUseID":"toolu_skill2",` +
			`"message":{"role":"user","content":"Approach this as a PM..."}}`,
		"",
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	events, err := ReadAll(path)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if len(events) != 2 || events[1].User == nil {
		t.Fatalf("events = %#v, want an assistant event followed by a user event", events)
	}

	user := events[1].User
	if user.SkillArgs != "build login page" {
		t.Errorf("SkillArgs = %q, want %q", user.SkillArgs, "build login page")
	}
}

// isMeta alone is not a skill marker — image placeholders and stop-hook
// feedback also carry it. A sourceToolUseID pointing at a non-Skill tool_use
// (or at nothing) must fall through to ordinary classification instead of
// being misclassified as a skill injection.
func TestReadAll_GivenIsMetaEntryNotLinkedToSkillToolUse_ThenNotClassifiedAsSkillInjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"assistant","timestamp":"2026-08-30T00:00:00Z","message":{"role":"assistant","content":[` +
			`{"type":"tool_use","id":"toolu_read1","name":"Read","input":{"file_path":"/repo/README.md"}}` +
			`]}}`,
		`{"type":"user","timestamp":"2026-08-30T00:00:01Z","isMeta":true,"sourceToolUseID":"toolu_read1",` +
			`"message":{"role":"user","content":"[Image #1]"}}`,
		"",
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	events, err := ReadAll(path)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if len(events) != 2 || events[1].User == nil {
		t.Fatalf("events = %#v, want an assistant event followed by a user event", events)
	}

	if user := events[1].User; user.IsSkillInjection {
		t.Errorf("IsSkillInjection = true, want false (sourceToolUseID resolves to a Read, not a Skill, tool_use)")
	}
}

// The stateless public ParseLine has no cross-line state to resolve
// sourceToolUseID against, so an isMeta entry with no "Base directory for
// this skill:" line falls through unclassified there — same behavior as
// before this fix. This pins that contract rather than silently changing it.
func TestParseLine_GivenSkillInjectionLinkWithoutBaseDirectoryLine_ThenNotClassified(t *testing.T) {
	event := parseLine(t, `{"type":"user","isMeta":true,"sourceToolUseID":"toolu_skill1",`+
		`"message":{"role":"user","content":"Approach this as the design lead..."}}`)
	if event.User == nil {
		t.Fatal("event.User = nil")
	}
	if event.User.IsSkillInjection {
		t.Error("IsSkillInjection = true, want false (ParseLine has no cross-line state to resolve the link)")
	}
	if event.User.Text != "Approach this as the design lead..." {
		t.Errorf("Text = %q, want the raw body preserved as a plain message", event.User.Text)
	}
}
