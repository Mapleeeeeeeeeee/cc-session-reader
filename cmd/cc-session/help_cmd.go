package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// cheatSheetRow is one row of the "intent -> command" quick reference
// printed by "cc-session help". It is a separate list from the command
// registry because some rows describe a flag variant of an existing command
// (e.g. read -verbose-bash) rather than a standalone subcommand, and the
// registry must not gain an entry for those.
type cheatSheetRow struct {
	intent  string
	command string
}

var cheatSheet = []cheatSheetRow{
	{"找目標 session", "cc-session list — 列出最近 session，-p 過濾專案，用過 cc-session 的標 [refs]"},
	{"讀 session（預設）", "cc-session inherit <id> — 分頁載入，重複呼叫翻頁"},
	{"查特定片段", "cc-session read <id> — 預設 200 行，-offset 跳讀"},
	{"緊湊單次輸出", "cc-session context <id> — 同 read 但更緊湊，帶 metadata header"},
	{"展開單一 tool call", "cc-session expand <id> <tool-id> — tool-id 取自輸出中的 [Tool#xxxx]"},
	{"展開同類所有 tool call", "cc-session read <id> -verbose-bash — 也有 -verbose-agents / -verbose-thinking"},
	{"分析 token 消耗", "cc-session stats <id>"},
	{"檢查過濾遺漏", "cc-session audit <id>"},
	{"查看 CLI 使用紀錄", "cc-session usage"},
}

func cmdHelp(args []string) {
	exitOnError(runHelp(args, os.Stdout, os.Stderr))
}

func runHelp(args []string, out io.Writer, errOut io.Writer) error {
	fs := flag.NewFlagSet("help", flag.ContinueOnError)
	fs.SetOutput(errOut)
	argumentHint := fs.Bool("argument-hint", false, "print a single-line [cmd | cmd ...] hint for a skill's argument-hint frontmatter")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *argumentHint {
		fmt.Fprintln(out, buildArgumentHint())
		return nil
	}

	// Rows are printed as "意圖 → 命令" rather than aligned columns: Go's
	// text/tabwriter pads by rune count, but CJK characters render two
	// columns wide in a terminal, so column-aligned output goes ragged.
	fmt.Fprintln(out, "cc-session 子命令速查表")
	fmt.Fprintln(out)
	for _, row := range cheatSheet {
		fmt.Fprintf(out, "%s → %s\n", row.intent, row.command)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Session ID 支援 prefix match，前 8 碼通常就夠。各子命令的 flags 用 -h 查看。")
	return nil
}

// buildArgumentHint renders the registry's non-hidden, hinted commands as a
// single "[a | b | c]" line for a Claude Code skill's argument-hint
// frontmatter. Commands with an empty argHint (e.g. "help", "benchmark") and
// hidden commands (e.g. "inject") are left out.
func buildArgumentHint() string {
	var hints []string
	for _, cmd := range commands {
		if cmd.hidden || cmd.argHint == "" {
			continue
		}
		hints = append(hints, cmd.argHint)
	}
	return "[" + strings.Join(hints, " | ") + "]"
}
