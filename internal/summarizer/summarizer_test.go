package summarizer

import (
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func toolInput(raw map[string]any) session.ToolInput {
	return session.ToolInput{Raw: raw}
}

func TestSummarizeToolUse_Bash(t *testing.T) {
	tests := []struct {
		name string
		inp  session.ToolInput
		want string
	}{
		{
			name: "with description",
			inp:  toolInput(map[string]any{"command": "ls -la /some/path", "description": "List files in directory"}),
			want: "[Bash] List files in directory",
		},
		{
			name: "without description",
			inp:  toolInput(map[string]any{"command": "ls -la /some/path"}),
			want: "[Bash] ls -la /some/path",
		},
		{
			name: "long command truncates by rune",
			inp:  toolInput(map[string]any{"command": strings.Repeat("世", 100)}),
			want: "[Bash] " + strings.Repeat("世", 80),
		},
		{
			name: "CJK under limit is not byte-truncated",
			inp:  toolInput(map[string]any{"command": strings.Repeat("世", 50)}),
			want: "[Bash] " + strings.Repeat("世", 50),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SummarizeToolUse("Bash", tt.inp, "")
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummarizeToolUse_Read(t *testing.T) {
	tests := []struct {
		name string
		path string
		inp  map[string]any
		cwd  string
		want string
	}{
		{name: "deep path shows last 2 segments", path: "/Users/maple/project/internal/parser/parser.go", cwd: "", want: "[Read] parser/parser.go"},
		{name: "single segment path", path: "file.txt", cwd: "", want: "[Read] file.txt"},
		{name: "empty path shows question mark", path: "", cwd: "", want: "[Read] ?"},
		{
			name: "relative path to cwd",
			path: "/Users/maple/project/src/features/api.ts",
			cwd:  "/Users/maple/project",
			want: "[Read] src/features/api.ts",
		},
		{
			name: "with line range limit only",
			path: "main.go",
			inp:  map[string]any{"limit": float64(100)},
			want: "[Read] main.go:1:100",
		},
		{
			name: "with line range offset and limit",
			path: "main.go",
			inp:  map[string]any{"offset": float64(20), "limit": float64(100)},
			want: "[Read] main.go:21:120",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := map[string]any{"file_path": tt.path}
			for k, v := range tt.inp {
				raw[k] = v
			}
			got := SummarizeToolUse("Read", toolInput(raw), tt.cwd)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummarizeToolUse_EditAndWrite(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		path     string
		cwd      string
		want     string
	}{
		{name: "Edit extracts filename from path", toolName: "Edit", path: "/Users/maple/project/main.go", cwd: "", want: "[Edit] project/main.go"},
		{name: "Write extracts filename from path", toolName: "Write", path: "/Users/maple/project/config.yaml", cwd: "", want: "[Write] project/config.yaml"},
		{name: "Edit with no slash returns full path", toolName: "Edit", path: "standalone.txt", cwd: "", want: "[Edit] standalone.txt"},
		{name: "Write with empty path shows question mark", toolName: "Write", path: "", cwd: "", want: "[Write] ?"},
		{
			name:     "Edit relative to cwd",
			toolName: "Edit",
			path:     "/Users/maple/project/src/main.go",
			cwd:      "/Users/maple/project",
			want:     "[Edit] src/main.go",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SummarizeToolUse(tt.toolName, toolInput(map[string]any{"file_path": tt.path}), tt.cwd)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummarizeToolUse_Agent(t *testing.T) {
	tests := []struct {
		name string
		inp  session.ToolInput
		want string
	}{
		{name: "with subagent_type", inp: toolInput(map[string]any{"description": "Explore codebase structure", "subagent_type": "explorer"}), want: "[Agent(explorer)] Explore codebase structure"},
		{name: "without subagent_type", inp: toolInput(map[string]any{"description": "Implement feature X"}), want: "[Agent] Implement feature X"},
		{name: "empty description fallback", inp: toolInput(nil), want: "[Agent] ?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SummarizeToolUse("Agent", tt.inp, "")
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummarizeToolUse_AskUserQuestion(t *testing.T) {
	inp := toolInput(map[string]any{
		"questions": []interface{}{
			map[string]interface{}{"question": "First question?"},
			map[string]interface{}{"question": "Second question?"},
			map[string]interface{}{"question": "Third question?"},
		},
	})
	got := SummarizeToolUse("AskUserQuestion", inp, "")
	want := "[AskUserQuestion] Q1: First question?\n  [AskUserQuestion] Q2: Second question?\n  [AskUserQuestion] Q3: Third question?"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSummarizeToolUse_AskUserQuestion_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		inp  session.ToolInput
		want string
	}{
		// Guards against panic or incorrect output when questions key is absent.
		{name: "given nil input map when summarizing then returns bare tag", inp: session.ToolInput{}, want: "[AskUserQuestion]"},
		// Guards against empty list being treated as valid questions.
		{name: "given empty questions list when summarizing then returns bare tag", inp: toolInput(map[string]any{"questions": []interface{}{}}), want: "[AskUserQuestion]"},
		// Guards against non-list value causing type assertion panic.
		{name: "given non-list questions value when summarizing then returns bare tag", inp: toolInput(map[string]any{"questions": "not a list"}), want: "[AskUserQuestion]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SummarizeToolUse("AskUserQuestion", tt.inp, "")
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummarizeToolUse_OtherTools(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		inp      session.ToolInput
		want     string
	}{
		{name: "Skill", toolName: "Skill", inp: toolInput(map[string]any{"skill": "pm", "args": "build login page"}), want: "[Skill] /pm build login page"},
		{name: "Grep with path", toolName: "Grep", inp: toolInput(map[string]any{"pattern": "TODO", "path": "./src"}), want: `[Grep] "TODO" in ./src`},
		{name: "Grep without path", toolName: "Grep", inp: toolInput(map[string]any{"pattern": "FIXME"}), want: `[Grep] "FIXME"`},
		{name: "Grep empty pattern", toolName: "Grep", inp: toolInput(nil), want: `[Grep] "?"`},
		{name: "Glob", toolName: "Glob", inp: toolInput(map[string]any{"pattern": "**/*.go"}), want: "[Glob] **/*.go"},
		{name: "ToolSearch", toolName: "ToolSearch", inp: toolInput(map[string]any{"query": "react docs"}), want: "[ToolSearch] react docs"},
		{name: "Unknown with no input", toolName: "WebSearch", inp: toolInput(nil), want: "[WebSearch]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SummarizeToolUse(tt.toolName, tt.inp, "")
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// --- ADR-003 decision 4: unknown-tool fallback shows input key/value pairs ---

// TestSummarizeToolUse_GivenUnknownToolWithInput_ThenShowsFirstKeysSortedAndTruncated
// guards the bug described in ADR-003: before this fix, a tool with no
// dedicated summarizer branch rendered "[ToolName]" with the input completely
// discarded, so a future Claude Code tool would silently lose all context.
// Map iteration order is not stable in Go, so this also pins that the key
// order in the rendered output is deterministic (alphabetical) rather than
// flaky across runs.
func TestSummarizeToolUse_GivenUnknownToolWithInput_ThenShowsFirstKeysSortedAndTruncated(t *testing.T) {
	tests := []struct {
		name string
		inp  session.ToolInput
		want string
	}{
		{
			name: "given input under the key cap then shows every key sorted alphabetically",
			inp:  toolInput(map[string]any{"query": "cats", "num_results": float64(5)}),
			want: "[WebSearch] num_results=5, query=cats",
		},
		{
			name: "given more keys than the cap then keeps only the first alphabetically",
			inp:  toolInput(map[string]any{"z_last": "z", "a_first": "a", "m_mid": "m", "b_second": "b"}),
			want: "[WebSearch] a_first=a, b_second=b, m_mid=m",
		},
		{
			name: "given a long value then truncates it to the 60-rune budget",
			inp:  toolInput(map[string]any{"payload": strings.Repeat("x", 100)}),
			want: "[WebSearch] payload=" + strings.Repeat("x", 60),
		},
		{
			name: "given empty input map then returns bare tag",
			inp:  toolInput(map[string]any{}),
			want: "[WebSearch]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SummarizeToolUse("WebSearch", tt.inp, "")
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
