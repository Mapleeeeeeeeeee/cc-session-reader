package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// cheatSheetRow is one row of the "intent -> command" quick reference
// printed by "cc-session help". intent and note are hand-written teaching
// text that has no home in the command registry; the "cc-session <name>
// <argHint>" portion is derived from the registry via commandText, so
// renaming a command or changing its argHint keeps this cheat sheet in sync
// automatically.
type cheatSheetRow struct {
	// intent is the plain-language "what do I want to do" column.
	intent string
	// cmdName looks up the row's registry entry by command.name to build
	// "cc-session <name> <argHint>". Empty for the one row that documents a
	// flag variant of an existing command (read -verbose-bash) rather than a
	// standalone registry entry; that row sets override instead.
	cmdName string
	// note is hand-written text appended after " — ", e.g. "-p 過濾專案".
	// Empty when the row has no aside.
	note string
	// override replaces the registry-derived command string outright. Only
	// the "-verbose-bash" row uses this, since it isn't a standalone
	// registry entry.
	override string
}

// commandText renders the row's "cc-session ..." command string, followed by
// " — note" when note is set.
func (r cheatSheetRow) commandText() string {
	command := r.override
	if command == "" {
		cmd, ok := findCommand(r.cmdName)
		if !ok {
			panic("cheatSheet: unknown command name " + r.cmdName)
		}
		// hidden commands (e.g. the deprecated "inject" alias) must never
		// surface in the help cheat sheet; see the hidden field's doc
		// comment in commands.go.
		if cmd.hidden {
			panic("cheatSheet: refuses to surface hidden command " + r.cmdName)
		}
		command = "cc-session " + cmd.argHint
	}
	if r.note == "" {
		return command
	}
	return command + " — " + r.note
}

var cheatSheet = []cheatSheetRow{
	{intent: "找目標 session", cmdName: "list", note: "列出最近 session，-p 過濾專案，用過 cc-session 的標 [refs]"},
	{intent: "讀 session（預設）", cmdName: "inherit", note: "分頁載入，重複呼叫翻頁"},
	{intent: "查特定片段", cmdName: "read", note: "預設 200 行，-offset 跳讀"},
	{intent: "緊湊單次輸出", cmdName: "context", note: "同 read 但更緊湊，帶 metadata header"},
	{intent: "展開單一 tool call", cmdName: "expand", note: "tool-id 取自輸出中的 [Tool#xxxx]"},
	{intent: "展開同類所有 tool call", override: "cc-session read <id> -verbose-bash", note: "也有 -verbose-agents / -verbose-thinking"},
	{intent: "分析 token 消耗", cmdName: "stats"},
	{intent: "檢查過濾遺漏", cmdName: "audit"},
	{intent: "查看 CLI 使用紀錄", cmdName: "usage"},
}

func cmdHelp(args []string) {
	exitOnError(runHelp(args, os.Stdout, os.Stderr))
}

func runHelp(args []string, out io.Writer, errOut io.Writer) error {
	fs := flag.NewFlagSet("help", flag.ContinueOnError)
	fs.SetOutput(errOut)
	isArgumentHint := fs.Bool("argument-hint", false, "print a single-line [cmd | cmd ...] hint for a skill's argument-hint frontmatter")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *isArgumentHint {
		fmt.Fprintln(out, buildArgumentHint())
		return nil
	}

	// Rows are printed as "意圖 → 命令" rather than aligned columns: Go's
	// text/tabwriter pads by rune count, but CJK characters render two
	// columns wide in a terminal, so column-aligned output goes ragged.
	fmt.Fprintln(out, "cc-session 子命令速查表")
	fmt.Fprintln(out)
	for _, row := range cheatSheet {
		fmt.Fprintf(out, "%s → %s\n", row.intent, row.commandText())
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
