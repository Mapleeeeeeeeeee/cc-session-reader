// Package driftscan finds record, attachment, system and content-block shapes
// in Claude Code transcripts that the reader has no explicit rule for. What
// counts as handled is decided by claudecodec, so the verdict cannot drift
// from what the reader actually does.
package driftscan

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/analyzer"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
)

// Category is the kind of shape a Finding belongs to.
type Category string

const (
	CategoryRecordType          Category = "record type"
	CategoryAttachmentType      Category = "attachment type"
	CategorySystemSubtype       Category = "system subtype"
	CategoryContentBlockType    Category = "content block type"
	CategoryToolResultBlockType Category = "tool_result block type"
)

// Categories lists every category in report order.
var Categories = []Category{
	CategoryRecordType,
	CategoryAttachmentType,
	CategorySystemSubtype,
	CategoryContentBlockType,
	CategoryToolResultBlockType,
}

// MissingValue stands in for a shape whose type field is absent or empty.
const MissingValue = "(missing)"

const (
	subagentsDirName = "subagents"
	// workflowJournalFileName is the append-only log a workflow keeps beside
	// its agents' transcripts. Its records (started, result, ...) are not
	// transcript records, so scanning it would report them as drift forever.
	workflowJournalFileName = "journal.jsonl"

	recordTypeAttachment = "attachment"
	recordTypeSystem     = "system"
	blockTypeToolResult  = "tool_result"
)

// Finding is one unhandled value and where it was first seen.
type Finding struct {
	Category Category
	Value    string
	// Records is how many records carry the value; a record with several
	// blocks of one type counts once.
	Records int
	// RecordsWithRole counts the records that carry message.role. Only
	// record types use it, for the ATTENTION rule.
	RecordsWithRole int
	// Sessions is how many distinct sessions contain the value; a subagent
	// transcript counts toward its parent session.
	Sessions int
	// FirstVersion and FirstTimestamp describe the earliest occurrence by
	// timestamp. Either is empty when no earlier line in that file carried it.
	FirstVersion   string
	FirstTimestamp string
	// ExampleFile and ExampleLine (1-based) locate the earliest occurrence.
	ExampleFile string
	ExampleLine int
}

// NeedsAttention applies audit's rule to an unlisted record type: it carries
// message.role, so it may be a new conversation type.
func (f Finding) NeedsAttention() bool {
	if f.Category != CategoryRecordType {
		return false
	}
	return analyzer.DiscardedTypeStats{
		Type:            f.Value,
		Records:         f.Records,
		RecordsWithRole: f.RecordsWithRole,
	}.NeedsAttention()
}

// Result is the outcome of one scan.
type Result struct {
	FilesScanned int
	Sessions     int
	// InvalidLines counts lines skipped because they were not valid JSON,
	// such as a half-written last line.
	InvalidLines int
	// UnreadableFiles counts files that could not be opened or read to the end.
	UnreadableFiles int
	// Findings is ordered by Categories, then record types that need
	// attention first, then by Records descending, then by Value.
	Findings []Finding
}

type findingKey struct {
	category Category
	value    string
}

type accumulator struct {
	finding  Finding
	sessions map[string]struct{}
}

type scanState struct {
	byKey map[findingKey]*accumulator
	// seenInRecord holds the values already counted for the record being
	// observed, so a record with several blocks of one type counts once.
	seenInRecord map[findingKey]struct{}
	sessions     map[string]struct{}
	invalidLines int
}

// Scan walks root for .jsonl transcripts modified at or after modifiedSince
// and reports the shapes the reader has no explicit rule for. It reads each
// file line by line and keeps only per-value counters, so memory does not
// grow with transcript size. A missing root is an error; unreadable files are
// counted in the result and skipped.
func Scan(root string, modifiedSince time.Time) (Result, error) {
	if _, err := os.Stat(root); err != nil {
		return Result{}, err
	}
	state := &scanState{
		byKey:        map[findingKey]*accumulator{},
		seenInRecord: map[findingKey]struct{}{},
		sessions:     map[string]struct{}{},
	}
	var result Result
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			result.UnreadableFiles++
			return nil
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" || entry.Name() == workflowJournalFileName {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			result.UnreadableFiles++
			return nil
		}
		if info.ModTime().Before(modifiedSince) {
			return nil
		}
		result.FilesScanned++
		if err := state.scanFile(path); err != nil {
			result.UnreadableFiles++
		}
		return nil
	})
	if walkErr != nil {
		return Result{}, walkErr
	}
	result.Sessions = len(state.sessions)
	result.InvalidLines = state.invalidLines
	result.Findings = state.sortedFindings()
	return result, nil
}

// sessionIDOf returns the session a transcript belongs to: the file name for
// a main transcript, the directory above "subagents" for a subagent's.
func sessionIDOf(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := len(parts) - 1; i > 0; i-- {
		if parts[i] == subagentsDirName {
			return parts[i-1]
		}
	}
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}

// recordShape is the part of a transcript line `drift` looks at. Content is
// kept raw so a record that is not conversation never pays for decoding it.
type recordShape struct {
	Type       string `json:"type"`
	Subtype    string `json:"subtype"`
	Timestamp  string `json:"timestamp"`
	Version    string `json:"version"`
	Attachment *struct {
		Type string `json:"type"`
	} `json:"attachment"`
	Message *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type blockShape struct {
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content"`
}

type occurrence struct {
	timestamp string
	version   string
	file      string
	line      int
}

func (s *scanState) scanFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sessionID := sessionIDOf(path)
	s.sessions[sessionID] = struct{}{}

	// A record without its own timestamp or version (api-request-blob has
	// neither) is dated by the latest value seen earlier in the same file.
	var lastTimestamp, lastVersion string
	reader := bufio.NewReader(f)
	for lineNumber := 1; ; lineNumber++ {
		line, readErr := reader.ReadBytes('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return fmt.Errorf("read %s: %w", path, readErr)
		}
		if len(bytes.TrimSpace(line)) > 0 {
			var record recordShape
			if err := json.Unmarshal(line, &record); err != nil {
				s.invalidLines++
			} else {
				if record.Timestamp != "" {
					lastTimestamp = record.Timestamp
				}
				if record.Version != "" {
					lastVersion = record.Version
				}
				s.observe(record, occurrence{
					timestamp: lastTimestamp,
					version:   lastVersion,
					file:      path,
					line:      lineNumber,
				}, sessionID)
			}
		}
		if readErr != nil {
			return nil
		}
	}
}

func (s *scanState) observe(record recordShape, at occurrence, sessionID string) {
	hasRole := record.Message != nil && record.Message.Role != ""
	clear(s.seenInRecord)

	if !claudecodec.IsHandledRecordType(record.Type) {
		s.add(CategoryRecordType, record.Type, hasRole, at, sessionID)
	}
	switch record.Type {
	case recordTypeAttachment:
		attachmentType := ""
		if record.Attachment != nil {
			attachmentType = record.Attachment.Type
		}
		if !claudecodec.IsKnownAttachmentType(attachmentType) {
			s.add(CategoryAttachmentType, attachmentType, false, at, sessionID)
		}
	case recordTypeSystem:
		if !claudecodec.IsKnownSystemSubtype(record.Subtype) {
			s.add(CategorySystemSubtype, record.Subtype, false, at, sessionID)
		}
	}

	if claudecodec.IsConversationRecordType(record.Type) && record.Message != nil {
		s.observeBlocks(record.Message.Content, CategoryContentBlockType,
			claudecodec.IsHandledContentBlockType, at, sessionID)
	}
}

// observeBlocks reports every block in a content array whose type is not
// handled, and descends into tool_result blocks for their own content array.
// A content that is not an array (a plain string) has no blocks.
func (s *scanState) observeBlocks(content json.RawMessage, category Category, isHandled func(string) bool, at occurrence, sessionID string) {
	var blocks []blockShape
	if err := json.Unmarshal(content, &blocks); err != nil {
		return
	}
	for _, block := range blocks {
		if !isHandled(block.Type) {
			s.add(category, block.Type, false, at, sessionID)
		}
		if category == CategoryContentBlockType && block.Type == blockTypeToolResult {
			s.observeBlocks(block.Content, CategoryToolResultBlockType,
				claudecodec.IsHandledToolResultBlockType, at, sessionID)
		}
	}
}

func (s *scanState) add(category Category, value string, hasRole bool, at occurrence, sessionID string) {
	if value == "" {
		value = MissingValue
	}
	key := findingKey{category: category, value: value}
	if _, counted := s.seenInRecord[key]; counted {
		return
	}
	s.seenInRecord[key] = struct{}{}
	acc, ok := s.byKey[key]
	if !ok {
		acc = &accumulator{
			finding:  Finding{Category: category, Value: value},
			sessions: map[string]struct{}{},
		}
		s.byKey[key] = acc
	}
	acc.finding.Records++
	if hasRole {
		acc.finding.RecordsWithRole++
	}
	acc.sessions[sessionID] = struct{}{}
	if acc.finding.Records == 1 || isEarlier(at, acc.finding) {
		acc.finding.FirstTimestamp = at.timestamp
		acc.finding.FirstVersion = at.version
		acc.finding.ExampleFile = at.file
		acc.finding.ExampleLine = at.line
	}
}

// isEarlier reports whether at precedes the finding's recorded first
// occurrence. An occurrence with no timestamp never replaces one with a
// timestamp; ties keep the first one seen.
func isEarlier(at occurrence, finding Finding) bool {
	if at.timestamp == "" {
		return false
	}
	return finding.FirstTimestamp == "" || at.timestamp < finding.FirstTimestamp
}

func (s *scanState) sortedFindings() []Finding {
	findings := make([]Finding, 0, len(s.byKey))
	for _, acc := range s.byKey {
		acc.finding.Sessions = len(acc.sessions)
		findings = append(findings, acc.finding)
	}
	categoryRank := map[Category]int{}
	for i, category := range Categories {
		categoryRank[category] = i
	}
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Category != b.Category {
			return categoryRank[a.Category] < categoryRank[b.Category]
		}
		if a.NeedsAttention() != b.NeedsAttention() {
			return a.NeedsAttention()
		}
		if a.Records != b.Records {
			return a.Records > b.Records
		}
		return a.Value < b.Value
	})
	return findings
}
