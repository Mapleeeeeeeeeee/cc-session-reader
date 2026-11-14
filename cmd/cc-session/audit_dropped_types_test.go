package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
)

const (
	droppedSectionHeader = "=== Dropped record types"
	nextSectionHeader    = "=== Samples ==="
)

// auditDroppedSection runs `audit` over a transcript made of the given extra
// lines after one user and one assistant record, and returns the dropped
// record types section of the output.
func auditDroppedSection(t *testing.T, extraLines ...string) string {
	t.Helper()
	root := t.TempDir()
	sid := "12345678-1234-1234-1234-123456789abc"
	projectDir := filepath.Join(root, "projects", "proj")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	lines := append([]string{
		`{"type":"user","timestamp":"2026-10-16T00:00:00Z","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","timestamp":"2026-10-16T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`,
	}, extraLines...)
	transcript := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(projectDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	var stdout, stderr bytes.Buffer
	store := parser.Store{ProjectsDir: filepath.Join(root, "projects")}
	if err := runAudit([]string{sid}, &stdout, &stderr, store, testReader); err != nil {
		t.Fatalf("runAudit returned error: %v", err)
	}

	out := stdout.String()
	start := strings.Index(out, droppedSectionHeader)
	end := strings.Index(out, nextSectionHeader)
	if start < 0 || end < start {
		t.Fatalf("dropped record types section not found in audit output:\n%s", out)
	}
	return out[start:end]
}

func Test_Audit_Given_DroppedRecordTypes_When_Audited_Then_SectionShowsTypeCountsAndAttentionFlag(t *testing.T) {
	blobUser := `{"type":"api-request-blob","hash":"a","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`
	blobAssistant := `{"type":"api-request-blob","hash":"b","message":{"role":"assistant","content":[{"type":"text","text":"hi 你好"}]}}`
	unknownWithRole := `{"type":"brand-new-conversation","message":{"role":"assistant","content":"x"}}`
	unknownWithoutMessage := `{"type":"brand-new-metadata","payload":{"value":1}}`

	tests := map[string]struct {
		extraLines []string
		wantRow    []string
		wantAbsent []string
	}{
		"a known request copy is listed with counts and not flagged": {
			extraLines: []string{blobUser, blobAssistant},
			wantRow: []string{
				"api-request-blob",
				"2 records",
				// The sum stays under 1,000, so the report prints it without a thousands separator.
				strconv.Itoa(utf8.RuneCountInString(blobUser)+utf8.RuneCountInString(blobAssistant)) + " chars",
				"2 with message.role",
				"known request copy or metadata",
			},
			wantAbsent: []string{"ATTENTION"},
		},
		"an unknown type carrying message.role is flagged": {
			extraLines: []string{unknownWithRole},
			wantRow: []string{
				"brand-new-conversation",
				"1 records",
				"1 with message.role",
				"ATTENTION",
			},
		},
		"an unknown type without message is listed but not flagged": {
			extraLines: []string{unknownWithoutMessage},
			wantRow: []string{
				"brand-new-metadata",
				"1 records",
				"0 with message.role",
				"unknown type, no message.role",
			},
			wantAbsent: []string{"ATTENTION"},
		},
		"a session with only conversation records reports nothing dropped": {
			wantRow:    []string{"no dropped record types in this session"},
			wantAbsent: []string{"ATTENTION", "records"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			section := auditDroppedSection(t, tt.extraLines...)
			for _, want := range tt.wantRow {
				if !strings.Contains(section, want) {
					t.Errorf("section missing %q\n%s", want, section)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(section, absent) {
					t.Errorf("section must not contain %q\n%s", absent, section)
				}
			}
		})
	}
}

func Test_Audit_Given_FlaggedAndKnownTypes_When_Audited_Then_FlaggedTypeIsListedFirst(t *testing.T) {
	bigKnown := `{"type":"api-request-blob","message":{"role":"user","content":"` + strings.Repeat("x", 500) + `"}}`
	smallUnknownWithRole := `{"type":"brand-new-conversation","message":{"role":"assistant","content":"x"}}`

	section := auditDroppedSection(t, bigKnown, smallUnknownWithRole)

	if flagged, known := strings.Index(section, "brand-new-conversation"), strings.Index(section, "api-request-blob"); flagged < 0 || known < 0 || flagged > known {
		t.Errorf("flagged type must come before the larger known type\n%s", section)
	}
}
