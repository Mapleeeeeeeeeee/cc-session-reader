// Package summarizer provides tool call one-line summaries.
package summarizer

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

const (
	maxCommandLen  = 80
	maxSkillLen    = 80
	maxQuestionLen = 90

	// unknownToolMaxKeys caps how many input keys are shown for a tool with no
	// dedicated summarizer branch (ADR-003 decision 4) — enough to identify
	// the call without dumping its whole payload.
	unknownToolMaxKeys = 3
	// unknownToolValueMaxLen truncates each shown value so a large payload
	// (e.g. a long prompt) can't blow up the one-line fallback summary.
	unknownToolValueMaxLen = 60
)

// CleanPath shortens an absolute file path for display: relative to cwd when
// possible, otherwise the last two path segments. Exported so callers that
// need to display the same short form outside a one-line tool summary (e.g.
// formatter's same-file-Read collapsing) don't have to reimplement it.
func CleanPath(path string, cwd string) string {
	if path == "" || path == "?" {
		return path
	}
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	return path
}

// SummarizeToolUse produces a one-line summary of a tool_use block.
func SummarizeToolUse(name string, inp session.ToolInput, cwd string) string {
	switch name {
	case session.ToolBash:
		desc := inp.String("description")
		if desc != "" {
			return fmt.Sprintf("[Bash] %s", desc)
		}
		cmd := inp.String("command")
		return fmt.Sprintf("[Bash] %s", session.Truncate(cmd, maxCommandLen))

	case session.ToolRead:
		path := inp.String("file_path")
		if path == "" {
			path = "?"
		}
		short := CleanPath(path, cwd)
		var offset, limit int
		var hasOffset, hasLimit bool
		if o, ok := inp.Raw["offset"]; ok {
			if f, ok := o.(float64); ok {
				offset = int(f)
				hasOffset = true
			} else if i, ok := o.(int); ok {
				offset = i
				hasOffset = true
			}
		}
		if l, ok := inp.Raw["limit"]; ok {
			if f, ok := l.(float64); ok {
				limit = int(f)
				hasLimit = true
			} else if i, ok := l.(int); ok {
				limit = i
				hasLimit = true
			}
		}
		if hasOffset && hasLimit {
			start := offset + 1
			end := offset + limit
			return fmt.Sprintf("[Read] %s:%d:%d", short, start, end)
		} else if hasLimit {
			return fmt.Sprintf("[Read] %s:1:%d", short, limit)
		}
		return fmt.Sprintf("[Read] %s", short)

	case session.ToolEdit, session.ToolWrite:
		path := inp.String("file_path")
		if path == "" {
			path = "?"
		}
		short := CleanPath(path, cwd)
		return fmt.Sprintf("[%s] %s", name, short)

	case session.ToolAgent:
		desc := inp.String("description")
		if desc == "" {
			desc = "?"
		}
		sub := inp.String("subagent_type")
		if sub != "" {
			return fmt.Sprintf("[Agent(%s)] %s", sub, desc)
		}
		return fmt.Sprintf("[Agent] %s", desc)

	case session.ToolGrep:
		pat := inp.String("pattern")
		if pat == "" {
			pat = "?"
		}
		path := inp.String("path")
		if path != "" {
			return fmt.Sprintf("[Grep] \"%s\" in %s", pat, path)
		}
		return fmt.Sprintf("[Grep] \"%s\"", pat)

	case session.ToolGlob:
		pat := inp.String("pattern")
		if pat == "" {
			pat = "?"
		}
		return fmt.Sprintf("[Glob] %s", pat)

	case session.ToolSkill:
		skill := inp.String("skill")
		if skill == "" {
			skill = "?"
		}
		args := inp.String("args")
		result := fmt.Sprintf("[Skill] /%s %s", skill, args)
		return session.Truncate(strings.TrimSpace(result), maxSkillLen)

	case session.ToolAskUserQuestion:
		qs, hasQuestions := inp.Raw["questions"]
		if !hasQuestions {
			return "[AskUserQuestion]"
		}
		qsList, isList := qs.([]any)
		if !isList || len(qsList) == 0 {
			return "[AskUserQuestion]"
		}
		var lines []string
		for i, q := range qsList {
			qMap, isMap := q.(map[string]any)
			if !isMap {
				continue
			}
			questionText, _ := qMap["question"].(string)
			if questionText == "" {
				questionText = "?"
			}
			line := fmt.Sprintf("[AskUserQuestion] Q%d: %s", i+1, questionText)
			lines = append(lines, session.Truncate(line, maxQuestionLen))
		}
		if len(lines) == 0 {
			return "[AskUserQuestion]"
		}
		return strings.Join(lines, "\n  ")

	case session.ToolSearch:
		query := inp.String("query")
		if query == "" {
			query = "?"
		}
		return fmt.Sprintf("[ToolSearch] %s", query)

	default:
		return fmt.Sprintf("[%s]%s", name, summarizeUnknownInput(inp))
	}
}

// summarizeUnknownInput renders the first few input key/value pairs for a
// tool with no dedicated summarizer branch (ADR-003 decision 4), so tools
// added to Claude Code after this release degrade gracefully instead of
// silently losing all input context. Returns "" for an empty input map.
// Keys are sorted alphabetically: map iteration order is not stable in Go,
// and an unstable key order would make the rendered summary flaky.
func summarizeUnknownInput(inp session.ToolInput) string {
	if len(inp.Raw) == 0 {
		return ""
	}
	keys := make([]string, 0, len(inp.Raw))
	for key := range inp.Raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > unknownToolMaxKeys {
		keys = keys[:unknownToolMaxKeys]
	}

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		value := session.Truncate(fmt.Sprintf("%v", inp.Raw[key]), unknownToolValueMaxLen)
		pairs = append(pairs, fmt.Sprintf("%s=%s", key, value))
	}
	return " " + strings.Join(pairs, ", ")
}
