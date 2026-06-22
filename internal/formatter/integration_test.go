package formatter

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/analyzer"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

const integrationFixture = "testdata/integration.jsonl"

// TestIntegration_FullPipeline_GivenRepeatedSkillInjection_WhenFormatted_ThenFirstIsCompressedAndSecondIsRepeat
// verifies that skill injections pass through the claudecodec→FormatReadEvents pipeline
// and are compressed to the one-line form, with repeated skills getting "(repeat)" suffix.
func TestIntegration_FullPipeline_GivenRepeatedSkillInjection_WhenFormatted_ThenFirstIsCompressedAndSecondIsRepeat(t *testing.T) {
	events, agentIDs := readIntegrationFixture(t)

	var out bytes.Buffer
	if err := FormatReadEvents(events, agentIDs, 0, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, "[skill: sessions] read abc123") {
		t.Errorf("first skill injection missing compact form\ngot:\n%s", got)
	}
	if !strings.Contains(got, "[skill: sessions] (repeat) read def456") {
		t.Errorf("second skill injection missing (repeat) marker\ngot:\n%s", got)
	}
	if strings.Contains(got, "Base directory for this skill") {
		t.Errorf("raw skill injection prefix must be stripped\ngot:\n%s", got)
	}
	if strings.Contains(got, "# Session Reader") {
		t.Errorf("skill body heading must be stripped\ngot:\n%s", got)
	}
}

// TestIntegration_FullPipeline_GivenCommandInjection_WhenFormatted_ThenCompressedToOneLine
// verifies the <command-message>/<command-name>/<command-args> XML block is collapsed
// to "/review check this code" by the pipeline.
func TestIntegration_FullPipeline_GivenCommandInjection_WhenFormatted_ThenCompressedToOneLine(t *testing.T) {
	events, agentIDs := readIntegrationFixture(t)

	var out bytes.Buffer
	if err := FormatReadEvents(events, agentIDs, 0, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, "/review check this code") {
		t.Errorf("command injection not compressed to one-line form\ngot:\n%s", got)
	}
	if strings.Contains(got, "<command-message>") {
		t.Errorf("<command-message> XML must be stripped\ngot:\n%s", got)
	}
	if strings.Contains(got, "<command-name>") {
		t.Errorf("<command-name> XML must be stripped\ngot:\n%s", got)
	}
}

// TestIntegration_FullPipeline_GivenTeammateMessage_WhenFormatted_ThenWarningStrippedBodyPreserved
// verifies teammate messages have the harness boilerplate removed while keeping
// the teammate ID, summary, and body content.
func TestIntegration_FullPipeline_GivenTeammateMessage_WhenFormatted_ThenWarningStrippedBodyPreserved(t *testing.T) {
	events, agentIDs := readIntegrationFixture(t)

	var out bytes.Buffer
	if err := FormatReadEvents(events, agentIDs, 0, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, `[teammate: reviewer-1 "Review done"]`) {
		t.Errorf("teammate compact header missing\ngot:\n%s", got)
	}
	if !strings.Contains(got, "Found 2 issues") {
		t.Errorf("teammate message body missing\ngot:\n%s", got)
	}
	if strings.Contains(got, "IMPORTANT: This is NOT from your user") {
		t.Errorf("harness warning boilerplate must be stripped\ngot:\n%s", got)
	}
}

// TestIntegration_FullPipeline_GivenSystemReminder_WhenFormatted_ThenEntireMessageDropped
// verifies <system-reminder> entries are completely suppressed — no tag, no body.
func TestIntegration_FullPipeline_GivenSystemReminder_WhenFormatted_ThenEntireMessageDropped(t *testing.T) {
	events, agentIDs := readIntegrationFixture(t)

	var out bytes.Buffer
	if err := FormatReadEvents(events, agentIDs, 0, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "<system-reminder>") {
		t.Errorf("<system-reminder> tag must be dropped\ngot:\n%s", got)
	}
	if strings.Contains(got, "task tools haven't been used") {
		t.Errorf("system-reminder body must be dropped\ngot:\n%s", got)
	}
}

// TestIntegration_FullPipeline_GivenContextUsageBlock_WhenFormatted_ThenEntireMessageDropped
// verifies the ## Context Usage token table is completely suppressed.
func TestIntegration_FullPipeline_GivenContextUsageBlock_WhenFormatted_ThenEntireMessageDropped(t *testing.T) {
	events, agentIDs := readIntegrationFixture(t)

	var out bytes.Buffer
	if err := FormatReadEvents(events, agentIDs, 0, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "## Context Usage") {
		t.Errorf("## Context Usage header must be dropped\ngot:\n%s", got)
	}
	if strings.Contains(got, "Estimated usage by category") {
		t.Errorf("context usage body must be dropped\ngot:\n%s", got)
	}
}

// TestIntegration_FullPipeline_GivenNormalConversation_WhenFormatted_ThenAllTextPreserved
// verifies that plain user and assistant turns survive the pipeline unchanged.
func TestIntegration_FullPipeline_GivenNormalConversation_WhenFormatted_ThenAllTextPreserved(t *testing.T) {
	events, agentIDs := readIntegrationFixture(t)

	var out bytes.Buffer
	if err := FormatReadEvents(events, agentIDs, 0, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents returned error: %v", err)
	}
	got := out.String()

	preserved := []string{
		"開始工作",
		"好的，開始了",
		"好，繼續",
		"繼續處理",
	}
	for _, want := range preserved {
		if !strings.Contains(got, want) {
			t.Errorf("normal conversation text %q missing from output\ngot:\n%s", want, got)
		}
	}

	// Bash tool use must produce a tool summary line.
	if !strings.Contains(got, "[Bash") {
		t.Errorf("Bash tool summary line missing\ngot:\n%s", got)
	}
}

// TestIntegration_FullPipeline_GivenFixture_WhenStatsComputed_ThenCompressionOccurredAndCategoriesPopulated
// verifies that ComputeStats reflects the compression the formatter performs:
// raw text is larger than filtered, and the key categories are non-zero.
func TestIntegration_FullPipeline_GivenFixture_WhenStatsComputed_ThenCompressionOccurredAndCategoriesPopulated(t *testing.T) {
	events, _ := readIntegrationFixture(t)

	stats := analyzer.ComputeStats(events)

	if stats.RawChars <= stats.FilteredChars {
		t.Errorf("expected raw chars > filtered chars (compression happened), got raw=%d filtered=%d",
			stats.RawChars, stats.FilteredChars)
	}
	if stats.Categories["user_text"] == 0 {
		t.Errorf("user_text category must be non-zero")
	}
	if stats.Categories["assistant_text"] == 0 {
		t.Errorf("assistant_text category must be non-zero")
	}
	if stats.Categories["tool_summaries"] == 0 {
		t.Errorf("tool_summaries category must be non-zero")
	}
}

// TestIntegration_TranscriptReaderInterface_GivenFixture_WhenReadViaCodecAndDirectly_ThenOutputIdentical
// verifies that claudecodec.Codec{}.ReadAll (via TranscriptReader interface) and
// claudecodec.ReadAll (direct package call) return identical events for the same fixture.
func TestIntegration_TranscriptReaderInterface_GivenFixture_WhenReadViaCodecAndDirectly_ThenOutputIdentical(t *testing.T) {
	viaInterface, err := claudecodec.Codec{}.ReadAll(integrationFixture)
	if err != nil {
		t.Fatalf("Codec{}.ReadAll returned error: %v", err)
	}
	direct, err := claudecodec.ReadAll(integrationFixture)
	if err != nil {
		t.Fatalf("claudecodec.ReadAll returned error: %v", err)
	}

	if len(viaInterface) != len(direct) {
		t.Fatalf("event count mismatch: interface=%d direct=%d", len(viaInterface), len(direct))
	}
	for i := range viaInterface {
		a, b := viaInterface[i], direct[i]
		if a.Kind != b.Kind {
			t.Errorf("event[%d] kind mismatch: %q vs %q", i, a.Kind, b.Kind)
		}
		if extractEventText(a) != extractEventText(b) {
			t.Errorf("event[%d] text mismatch:\ninterface: %q\ndirect:    %q",
				i, extractEventText(a), extractEventText(b))
		}
	}
}

// readIntegrationFixture reads the shared JSONL fixture via claudecodec.Codec
// and returns events plus an empty agentIDs map (fixture has no Agent tool calls).
func readIntegrationFixture(t *testing.T) ([]session.Event, map[string]bool) {
	t.Helper()
	events, err := claudecodec.Codec{}.ReadAll(integrationFixture)
	if err != nil {
		t.Fatalf("read integration fixture: %v", err)
	}
	return events, map[string]bool{}
}

// TestIntegration_LocalSession_GivenConfiguredSession_WhenFormatted_ThenProducesNonEmptyOutput
// runs the full pipeline against a real local session specified in config.json.
// Skips if no integration_test_session is configured.
func TestIntegration_LocalSession_GivenConfiguredSession_WhenFormatted_ThenProducesNonEmptyOutput(t *testing.T) {
	sid := loadLocalSessionID()
	if sid == "" {
		t.Skip("no integration_test_session in ~/.claude/skills/cc-session/config.json")
	}

	store := parser.DefaultStore()
	resolved, err := store.ResolveSession(sid)
	if err != nil {
		t.Fatalf("resolve session %q: %v", sid, err)
	}

	codec := claudecodec.Codec{}
	events, err := codec.ReadAll(resolved.Path)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("local session has 0 events")
	}

	agentIDs := session.CollectAgentToolIDs(events)
	var out bytes.Buffer
	if err := FormatReadEvents(events, agentIDs, 0, 0, FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatReadEvents: %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("formatted output is empty for local session")
	}

	got := out.String()

	// Verify no raw harness boilerplate leaked through.
	// NOTE: these are negative checks — they only catch regressions if the
	// configured session actually contains the corresponding boilerplate types
	// (skill injection, teammate messages, etc). Choose a session that exercises
	// multiple harness features for maximum coverage.
	if strings.Contains(got, "Base directory for this skill:") {
		t.Error("skill injection not compressed in local session")
	}
	if strings.Contains(got, "IMPORTANT: This is NOT from your user") {
		t.Error("teammate warning boilerplate not stripped in local session")
	}
	if strings.Contains(got, "<command-name>") {
		t.Error("command injection XML not stripped in local session")
	}
	if strings.Contains(got, "## Context Usage") {
		t.Error("context usage header not stripped in local session")
	}
	if strings.Contains(got, "Estimated usage by category") {
		t.Error("context usage body not stripped in local session")
	}

	t.Logf("local session %s: %d events → %d chars formatted", sid, len(events), out.Len())
}

func loadLocalSessionID() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "cc-session", "config.json"))
	if err != nil {
		return ""
	}
	var cfg struct {
		IntegrationTestSession string `json:"integration_test_session"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return ""
	}
	return cfg.IntegrationTestSession
}

// extractEventText returns the primary text content of an event for comparison.
func extractEventText(e session.Event) string {
	switch e.Kind {
	case session.EventUserMessage:
		if e.User != nil {
			return e.User.Text + e.User.CommandMarker + e.User.SkillName + e.User.SkillArgs
		}
	case session.EventAssistantMessage:
		if e.Assistant != nil {
			return e.Assistant.Text
		}
	case session.EventToolResult:
		if e.Tool != nil {
			return e.Tool.Text
		}
	}
	return ""
}
