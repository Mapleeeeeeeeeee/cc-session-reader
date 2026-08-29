package summarizer

import (
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

// ADR-007 decision 2: the Bash summary dropped the command entirely, leaving
// the reader unable to tell what actually ran. A plain prefix of the command
// does not fix it, because real commands open with scaffolding: these cases
// are the shapes that made a naive 20-character prefix useless.

func TestCommandVerb_GivenScaffoldedCommand_WhenExtracted_ThenNamesTheRealProgram(t *testing.T) {
	tests := map[string]struct {
		command string
		want    string
	}{
		"a bare command is its own verb": {
			command: "pnpm tsc --noEmit",
			want:    "pnpm tsc",
		},
		"a leading cd is skipped": {
			command: "cd /Users/maple/Desktop/nccu-toolkit && git push --force",
			want:    "git push",
		},
		"a leading env assignment is skipped": {
			command: "NODE_ENV=test SEED=42 npx vitest run",
			want:    "npx vitest",
		},
		"an echoed heading is skipped": {
			command: `echo "=== game-engine ===" && sed -n '210,225p' src/engine.ts`,
			want:    "sed -n",
		},
		"a pipeline reports the program that produces the data": {
			command: "grep -n TODO src/*.ts | head -20",
			want:    "grep -n",
		},
		"an absolute interpreter path is reduced to its base name": {
			command: "/opt/homebrew/bin/gh pr view 407 --json state",
			want:    "gh pr",
		},
		"a comment line is skipped for the command below it": {
			command: "# Search only main session files\nrg --files-with-matches race",
			want:    "rg --files-with-matches",
		},
		"a poll loop reports the program it polls with": {
			command: "until /opt/homebrew/bin/gh pr checks 409; do sleep 30; done",
			want:    "gh pr",
		},
		"a program with no arguments is still a verb": {
			command: "cd /tmp && ls",
			want:    "ls",
		},
		"scaffolding alone yields nothing to name": {
			command: "cd /Users/maple/Desktop && export PATH=/usr/bin",
			want:    "",
		},
		"an empty command yields nothing": {
			command: "",
			want:    "",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := commandVerb(tc.command); got != tc.want {
				t.Errorf("commandVerb(%q) = %q, want %q", tc.command, got, tc.want)
			}
		})
	}
}

func TestSummarizeToolUse_GivenBashWithDescription_WhenSummarized_ThenCarriesBothDescriptionAndVerb(t *testing.T) {
	input := session.ToolInput{Raw: map[string]any{
		"description": "查 PR #407 狀態與 CI",
		"command":     "cd /Users/maple/Desktop/nccu-toolkit && gh pr view 407 --json state",
	}}

	if got, want := SummarizeToolUse("Bash", input, ""), "[Bash] 查 PR #407 狀態與 CI | gh pr"; got != want {
		t.Errorf("SummarizeToolUse() = %q, want %q", got, want)
	}
}

// When the command is nothing but scaffolding there is no verb to add, and
// the summary must not end in a dangling separator.
func TestSummarizeToolUse_GivenBashWhoseCommandIsAllScaffolding_WhenSummarized_ThenOmitsTheSeparator(t *testing.T) {
	input := session.ToolInput{Raw: map[string]any{
		"description": "切到專案目錄",
		"command":     "cd /Users/maple/Desktop/nccu-toolkit",
	}}

	if got, want := SummarizeToolUse("Bash", input, ""), "[Bash] 切到專案目錄"; got != want {
		t.Errorf("SummarizeToolUse() = %q, want %q", got, want)
	}
}

func TestSummarizeToolUse_GivenBashWithLongVerb_WhenSummarized_ThenTruncatesToTheVerbBudget(t *testing.T) {
	input := session.ToolInput{Raw: map[string]any{
		"description": "跑覆蓋率",
		"command":     "npx " + strings.Repeat("v", 60),
	}}

	got := SummarizeToolUse("Bash", input, "")
	verb := strings.TrimPrefix(got, "[Bash] 跑覆蓋率 | ")
	if len([]rune(verb)) != maxVerbLen {
		t.Errorf("verb %q has %d runes, want %d", verb, len([]rune(verb)), maxVerbLen)
	}
}
