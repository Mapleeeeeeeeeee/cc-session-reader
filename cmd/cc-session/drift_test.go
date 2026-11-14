package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
)

const (
	driftSessionA = "aaaaaaaa-0000-0000-0000-000000000001"
	driftSessionB = "bbbbbbbb-0000-0000-0000-000000000002"
)

// driftTranscript is one JSONL file to place under a fake ~/.claude/projects.
type driftTranscript struct {
	relPath string
	lines   []string
	// age is how long ago the file was last modified; zero means just now.
	age time.Duration
}

// runDriftOn writes the transcripts into a temp projects directory, runs
// `drift` with args and returns its output plus the projects directory so a
// test can build the expected example path.
func runDriftOn(t *testing.T, args []string, transcripts ...driftTranscript) (string, string) {
	t.Helper()
	projectsDir := filepath.Join(t.TempDir(), "projects")
	for _, transcript := range transcripts {
		path := filepath.Join(projectsDir, filepath.FromSlash(transcript.relPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create transcript dir: %v", err)
		}
		content := strings.Join(transcript.lines, "\n") + "\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write transcript: %v", err)
		}
		modTime := time.Now().Add(-transcript.age)
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatalf("set transcript mtime: %v", err)
		}
	}

	var stdout, stderr bytes.Buffer
	if err := runDrift(args, &stdout, &stderr, parser.Store{ProjectsDir: projectsDir}); err != nil {
		t.Fatalf("runDrift returned error: %v", err)
	}
	return stdout.String(), projectsDir
}

// driftSection returns the part of the output under one "=== Unhandled ...
// ===" heading.
func driftSection(t *testing.T, output string, heading string) string {
	t.Helper()
	marker := "=== Unhandled " + heading + " ==="
	start := strings.Index(output, marker)
	if start < 0 {
		t.Fatalf("section %q not found in output:\n%s", heading, output)
	}
	rest := output[start+len(marker):]
	if end := strings.Index(rest, "\n=== "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

const driftUserLine = `{"type":"user","timestamp":"2026-10-01T00:00:00Z","version":"2.1.300","message":{"role":"user","content":"hello"}}`

func Test_Drift_Given_AnUnhandledValueInEachCategory_When_Scanned_Then_ItIsListedUnderItsSection(t *testing.T) {
	tests := []struct {
		name    string
		heading string
		line    string
		value   string
	}{
		{
			name:    "record type",
			heading: "record types",
			line:    `{"type":"brand-new-record","timestamp":"2026-10-02T00:00:00Z","version":"2.1.301","payload":1}`,
			value:   "brand-new-record",
		},
		{
			name:    "attachment type",
			heading: "attachment types",
			line:    `{"type":"attachment","timestamp":"2026-10-02T00:00:00Z","version":"2.1.301","attachment":{"type":"brand_new_attachment"}}`,
			value:   "brand_new_attachment",
		},
		{
			name:    "system subtype",
			heading: "system subtypes",
			line:    `{"type":"system","subtype":"brand_new_subtype","timestamp":"2026-10-02T00:00:00Z","version":"2.1.301"}`,
			value:   "brand_new_subtype",
		},
		{
			name:    "content block type",
			heading: "content block types",
			line:    `{"type":"user","timestamp":"2026-10-02T00:00:00Z","version":"2.1.301","message":{"role":"user","content":[{"type":"brand_new_block"}]}}`,
			value:   "brand_new_block",
		},
		{
			name:    "tool_result block type",
			heading: "tool_result block types",
			line:    `{"type":"user","timestamp":"2026-10-02T00:00:00Z","version":"2.1.301","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"brand_new_inner"}]}]}}`,
			value:   "brand_new_inner",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, projectsDir := runDriftOn(t, nil, driftTranscript{
				relPath: "proj/" + driftSessionA + ".jsonl",
				lines:   []string{driftUserLine, tt.line},
			})

			section := driftSection(t, output, tt.heading)
			wantExample := filepath.Join(projectsDir, "proj", driftSessionA+".jsonl") + ":2"
			for _, want := range []string{tt.value, "1 records", "1 sessions", "2.1.301", "2026-10-02", wantExample} {
				if !strings.Contains(section, want) {
					t.Errorf("section %q missing %q, got:\n%s", tt.heading, want, section)
				}
			}
		})
	}
}

func Test_Drift_Given_TheSameValueInTwoSessions_When_Scanned_Then_CountsBothAndReportsTheEarliestOccurrence(t *testing.T) {
	later := `{"type":"brand-new-record","timestamp":"2026-10-05T00:00:00Z","version":"2.1.305"}`
	earlier := `{"type":"brand-new-record","timestamp":"2026-10-03T00:00:00Z","version":"2.1.303"}`
	// Session A sorts first in the directory walk but holds the later occurrence.
	output, projectsDir := runDriftOn(t, nil,
		driftTranscript{relPath: "proj/" + driftSessionA + ".jsonl", lines: []string{driftUserLine, later, later}},
		driftTranscript{relPath: "proj/" + driftSessionB + ".jsonl", lines: []string{earlier}},
	)

	section := driftSection(t, output, "record types")
	wantExample := filepath.Join(projectsDir, "proj", driftSessionB+".jsonl") + ":1"
	for _, want := range []string{"brand-new-record", "3 records", "2 sessions", "first seen in 2.1.303 on 2026-10-03", wantExample} {
		if !strings.Contains(section, want) {
			t.Errorf("record types section missing %q, got:\n%s", want, section)
		}
	}
}

func Test_Drift_Given_OneRecordWithSeveralBlocksOfAnUnhandledType_When_Scanned_Then_ItCountsAsOneRecord(t *testing.T) {
	twoImages := `{"type":"user","timestamp":"2026-10-02T00:00:00Z","message":{"role":"user","content":[{"type":"image"},{"type":"image"}]}}`
	output, _ := runDriftOn(t, nil, driftTranscript{
		relPath: "proj/" + driftSessionA + ".jsonl",
		lines:   []string{twoImages},
	})

	section := driftSection(t, output, "content block types")
	if !strings.Contains(section, "1 records") {
		t.Errorf("want 1 record, got:\n%s", section)
	}
}

func Test_Drift_Given_ASubagentTranscript_When_Scanned_Then_ItCountsTowardItsParentSession(t *testing.T) {
	unknown := `{"type":"brand-new-record","timestamp":"2026-10-03T00:00:00Z","version":"2.1.303"}`
	output, _ := runDriftOn(t, nil,
		driftTranscript{relPath: "proj/" + driftSessionA + ".jsonl", lines: []string{unknown}},
		driftTranscript{relPath: "proj/" + driftSessionA + "/subagents/agent-1.jsonl", lines: []string{unknown}},
	)

	section := driftSection(t, output, "record types")
	if !strings.Contains(section, "2 records") || !strings.Contains(section, "1 sessions") {
		t.Errorf("want 2 records in 1 session, got:\n%s", section)
	}
}

func Test_Drift_Given_OnlyHandledValues_When_Scanned_Then_SaysNothingIsUnhandled(t *testing.T) {
	handled := []string{
		`{"type":"assistant","timestamp":"2026-10-01T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hi"},{"type":"thinking","thinking":"hm"},{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}`,
		`{"type":"user","timestamp":"2026-10-01T00:00:02Z","toolUseResult":{},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"ok"}]}]}}`,
		`{"type":"progress","timestamp":"2026-10-01T00:00:03Z"}`,
		`{"type":"api-request-blob","hash":"h","message":{"role":"user","content":[{"type":"text","text":"copy"}]}}`,
		`{"type":"attachment","timestamp":"2026-10-01T00:00:04Z","attachment":{"type":"environment"}}`,
		`{"type":"system","subtype":"turn_duration","timestamp":"2026-10-01T00:00:05Z"}`,
		`{"type":"system","subtype":"compact_boundary","timestamp":"2026-10-01T00:00:06Z"}`,
	}
	output, _ := runDriftOn(t, nil, driftTranscript{
		relPath: "proj/" + driftSessionA + ".jsonl",
		lines:   append([]string{driftUserLine}, handled...),
	})

	if !strings.Contains(output, "every shape in these transcripts is already handled") {
		t.Errorf("output does not say everything is handled:\n%s", output)
	}
	if got := strings.Count(output, "  none\n"); got != 5 {
		t.Errorf("want all 5 sections to say none, got %d in:\n%s", got, output)
	}
}

func Test_Drift_Given_UnlistedRecordTypes_When_Scanned_Then_OnlyTheOneCarryingMessageRoleIsAttention(t *testing.T) {
	withRole := `{"type":"brand-new-conversation","timestamp":"2026-10-02T00:00:00Z","message":{"role":"assistant","content":"x"}}`
	withoutRole := `{"type":"brand-new-metadata","timestamp":"2026-10-02T00:00:00Z","payload":1}`
	output, _ := runDriftOn(t, nil, driftTranscript{
		relPath: "proj/" + driftSessionA + ".jsonl",
		lines:   []string{withoutRole, withRole},
	})

	section := driftSection(t, output, "record types")
	var withRoleLine, withoutRoleLine string
	for _, line := range strings.Split(section, "\n") {
		switch {
		case strings.Contains(line, "brand-new-conversation"):
			withRoleLine = line
		case strings.Contains(line, "brand-new-metadata"):
			withoutRoleLine = line
		}
	}
	if !strings.Contains(withRoleLine, "ATTENTION") {
		t.Errorf("type with message.role should be flagged, got %q", withRoleLine)
	}
	if withoutRoleLine == "" || strings.Contains(withoutRoleLine, "ATTENTION") {
		t.Errorf("type without message.role should be listed unflagged, got %q", withoutRoleLine)
	}
	if strings.Index(section, "brand-new-conversation") > strings.Index(section, "brand-new-metadata") {
		t.Errorf("ATTENTION type should be listed first, got:\n%s", section)
	}
}

func Test_Drift_Given_AnOldTranscript_When_DaysIsShorterThanItsAge_Then_ItIsSkipped(t *testing.T) {
	unknown := `{"type":"brand-new-record","timestamp":"2026-08-01T00:00:00Z"}`
	const fortyDays = 40 * 24 * time.Hour
	old := driftTranscript{relPath: "proj/" + driftSessionA + ".jsonl", lines: []string{unknown}, age: fortyDays}

	tests := []struct {
		name        string
		args        []string
		wantListed  bool
		wantScanned string
	}{
		{name: "default window excludes it", args: nil, wantListed: false, wantScanned: "scanned 0 files"},
		{name: "explicit shorter window excludes it", args: []string{"-days", "7"}, wantListed: false, wantScanned: "scanned 0 files"},
		{name: "longer window includes it", args: []string{"-days", "60"}, wantListed: true, wantScanned: "scanned 1 files"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, _ := runDriftOn(t, tt.args, old)
			if got := strings.Contains(output, "brand-new-record"); got != tt.wantListed {
				t.Errorf("listed = %v, want %v, output:\n%s", got, tt.wantListed, output)
			}
			if !strings.Contains(output, tt.wantScanned) {
				t.Errorf("output missing %q:\n%s", tt.wantScanned, output)
			}
		})
	}
}

func Test_Drift_Given_AWorkflowJournal_When_Scanned_Then_ItIsNotCountedAsATranscript(t *testing.T) {
	journal := driftTranscript{
		relPath: "proj/" + driftSessionA + "/subagents/workflows/wf_1/journal.jsonl",
		lines:   []string{`{"type":"started","key":"k","agentId":"a"}`},
	}
	output, _ := runDriftOn(t, nil, journal)

	if !strings.Contains(output, "scanned 0 files") {
		t.Errorf("journal should not be scanned:\n%s", output)
	}
}

func Test_Drift_Given_AnUnparsableLine_When_Scanned_Then_ItIsSkippedAndCounted(t *testing.T) {
	unknown := `{"type":"brand-new-record","timestamp":"2026-10-03T00:00:00Z"}`
	output, _ := runDriftOn(t, nil, driftTranscript{
		relPath: "proj/" + driftSessionA + ".jsonl",
		lines:   []string{`{"type":"user","message":{"role":"us`, unknown},
	})

	if !strings.Contains(output, "skipped 1 lines that were not valid JSON") {
		t.Errorf("output does not report the invalid line:\n%s", output)
	}
	if !strings.Contains(output, "brand-new-record") {
		t.Errorf("the valid line after it should still be scanned:\n%s", output)
	}
}

func Test_Drift_Given_ANonPositiveDays_When_Run_Then_ReturnsAnError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runDrift([]string{"-days", "0"}, &stdout, &stderr, parser.Store{ProjectsDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "-days must be a positive integer") {
		t.Fatalf("want a -days error, got %v", err)
	}
}

// Drift only reports. This pins `read` on a transcript full of the shapes
// drift lists (unknown record, attachment and system values, image blocks,
// tool_result image blocks): none of them may change what `read` prints.
func Test_Drift_Given_TranscriptWithUnhandledShapes_When_Read_Then_OutputIsUnchanged(t *testing.T) {
	sid := "12345678-1234-1234-1234-123456789abc"
	lines := []string{
		`{"type":"brand-new-record","timestamp":"2026-10-01T00:00:00Z","payload":1}`,
		`{"type":"attachment","timestamp":"2026-10-01T00:00:01Z","attachment":{"type":"brand_new_attachment"}}`,
		`{"type":"system","subtype":"brand_new_subtype","timestamp":"2026-10-01T00:00:02Z"}`,
		`{"type":"user","timestamp":"2026-10-01T10:00:03Z","message":{"role":"user","content":[{"type":"text","text":"look at this"},{"type":"image","source":{"type":"base64","data":"AAAA"}}]}}`,
		`{"type":"assistant","timestamp":"2026-10-01T10:00:04Z","message":{"role":"assistant","content":[{"type":"brand_new_block"},{"type":"text","text":"I see it"},{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"/tmp/a.png"}}]}}`,
		`{"type":"user","timestamp":"2026-10-01T10:00:05Z","toolUseResult":{},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"image","source":{"type":"base64","data":"AAAA"}},{"type":"text","text":"a picture"}]}]}}`,
	}
	root := t.TempDir()
	projectDir := filepath.Join(root, "projects", "proj")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	var stdout, stderr bytes.Buffer
	store := parser.Store{ProjectsDir: filepath.Join(root, "projects")}
	if err := runRead([]string{sid, "-max-lines", "0"}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runRead returned error: %v", err)
	}

	const want = "--- 2026-10-01 ---\n\n[10:00:03] user:\nlook at this\n\n[10:00:04] assistant:\nI see it\n  [Read#lu_1] tmp/a.png\n\n"
	if got := stdout.String(); got != want {
		t.Errorf("read output changed.\ngot:\n%q\nwant:\n%q", got, want)
	}
}
