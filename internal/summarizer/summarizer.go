// Package summarizer provides tool call one-line summaries.
package summarizer

import (
	"fmt"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

const (
	maxCommandLen  = 80
	maxSkillLen    = 80
	maxQuestionLen = 90
)

// SummarizeToolUse produces a one-line summary of a tool_use block.
func SummarizeToolUse(name string, inp session.ToolInput) string {
	switch name {
	case "Bash":
		desc := inp.String("description")
		if desc != "" {
			return fmt.Sprintf("[Bash] %s", desc)
		}
		cmd := inp.String("command")
		return fmt.Sprintf("[Bash] %s", session.Truncate(cmd, maxCommandLen))

	case "Read":
		path := inp.String("file_path")
		if path == "" {
			path = "?"
		}
		parts := strings.Split(path, "/")
		var short string
		if len(parts) >= 2 {
			short = strings.Join(parts[len(parts)-2:], "/")
		} else {
			short = path
		}
		return fmt.Sprintf("[Read] %s", short)

	case "Edit", "Write":
		path := inp.String("file_path")
		if path == "" {
			path = "?"
		}
		filename := path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			filename = path[idx+1:]
		}
		return fmt.Sprintf("[%s] %s", name, filename)

	case "Agent":
		desc := inp.String("description")
		if desc == "" {
			desc = "?"
		}
		sub := inp.String("subagent_type")
		if sub != "" {
			return fmt.Sprintf("[Agent(%s)] %s", sub, desc)
		}
		return fmt.Sprintf("[Agent] %s", desc)

	case "Grep":
		pat := inp.String("pattern")
		if pat == "" {
			pat = "?"
		}
		path := inp.String("path")
		if path != "" {
			return fmt.Sprintf("[Grep] \"%s\" in %s", pat, path)
		}
		return fmt.Sprintf("[Grep] \"%s\"", pat)

	case "Glob":
		pat := inp.String("pattern")
		if pat == "" {
			pat = "?"
		}
		return fmt.Sprintf("[Glob] %s", pat)

	case "Skill":
		skill := inp.String("skill")
		if skill == "" {
			skill = "?"
		}
		args := inp.String("args")
		result := fmt.Sprintf("[Skill] /%s %s", skill, args)
		return session.Truncate(strings.TrimSpace(result), maxSkillLen)

	case "AskUserQuestion":
		qs, hasQuestions := inp.Raw["questions"]
		if !hasQuestions {
			return "[AskUserQuestion]"
		}
		qsList, isList := qs.([]interface{})
		if !isList || len(qsList) == 0 {
			return "[AskUserQuestion]"
		}
		var lines []string
		for i, q := range qsList {
			qMap, isMap := q.(map[string]interface{})
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

	case "ToolSearch":
		query := inp.String("query")
		if query == "" {
			query = "?"
		}
		return fmt.Sprintf("[ToolSearch] %s", query)

	default:
		return fmt.Sprintf("[%s]", name)
	}
}
