// Package summarizer provides tool call one-line summaries.
package summarizer

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

const (
	maxCommandLen  = 80
	maxSkillLen    = 80
	maxQuestionLen = 90
)

func cleanPath(path string, cwd string) string {
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
		short := cleanPath(path, cwd)
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
		short := cleanPath(path, cwd)
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
		return fmt.Sprintf("[%s]", name)
	}
}
