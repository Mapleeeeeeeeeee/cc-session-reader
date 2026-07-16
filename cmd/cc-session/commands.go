package main

import (
	"fmt"
	"os"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
)

// command describes one cc-session subcommand. It is the single source of
// truth for dispatch (main's switch), printUsage, and --argument-hint —
// those used to hand-copy the command list independently and drift out of
// sync. The "cc-session help" cheat sheet in help_cmd.go also derives each
// row's "cc-session <name> <argHint>" fragment from this registry by name,
// but keeps its intent text and teaching asides hand-written since those
// aren't part of the registry.
type command struct {
	// name is the word typed after "cc-session" (os.Args[1]) and the label
	// shown in printUsage / help.
	name string
	// summary is the one-line Chinese description shown next to name in
	// printUsage's "Commands:" block.
	summary string
	// argHint is the input-box fragment shown in "cc-session help
	// --argument-hint", e.g. "read <id>". Empty means the command is left out
	// of that line (it still appears in printUsage/help unless hidden) —
	// used for meta commands like "help" and "benchmark" that aren't part of
	// the skill's quick-launch surface.
	argHint string
	// hidden excludes the command from printUsage, the argument-hint line,
	// and the help cheat sheet, while keeping it dispatchable. Used for the
	// deprecated "inject" alias. The cheat sheet enforces this by panicking
	// if a row ever references a hidden command's name.
	hidden bool
	// tracksUsage marks commands whose invocations should be recorded to
	// usage.jsonl. main's dispatch starts tracking (see beginUsageTracking
	// in usage_cmd.go) before run executes, so a failure anywhere inside
	// run — including before it resolves a session ID — still produces a
	// usage entry. Meta commands ("help", "usage" itself) leave this false.
	tracksUsage bool
	// run executes the command. reader is the concrete claudecodec.Codec,
	// which satisfies both session.TranscriptReader and session.HeaderScanner
	// so one signature covers every cmdXxx regardless of which interface it
	// actually needs.
	run func(args []string, reader claudecodec.Codec)
}

// commands is the command registry, in the order printUsage lists them.
//
// It is populated from init() rather than the var initializer itself: the
// "help" entry's run closure calls into buildArgumentHint, which reads this
// same var, and Go's package-init cycle detector treats that as a cycle when
// it's part of the var's initializer expression. Assigning inside init()
// keeps the declaration a plain nil-slice zero value, so there is nothing
// for the cycle check to trip on.
var commands []command

func init() {
	commands = []command{
		{
			name:        "list",
			summary:     "列出最近的 session",
			argHint:     "list",
			tracksUsage: true,
			run:         func(args []string, reader claudecodec.Codec) { cmdList(args, reader) },
		},
		{
			name:        "inherit",
			summary:     "分頁繼承 session 到 context",
			argHint:     "inherit <id>",
			tracksUsage: true,
			run:         func(args []string, reader claudecodec.Codec) { cmdInherit(args, reader) },
		},
		{
			name:        "read",
			summary:     "完整對話 + tool call 一行摘要",
			argHint:     "read <id>",
			tracksUsage: true,
			run:         func(args []string, reader claudecodec.Codec) { cmdRead(args, reader) },
		},
		{
			name:        "context",
			summary:     "精簡注入格式（帶 metadata header）",
			argHint:     "context <id>",
			tracksUsage: true,
			run:         func(args []string, reader claudecodec.Codec) { cmdContext(args, reader) },
		},
		{
			name:        "expand",
			summary:     "展開特定 tool call 完整內容",
			argHint:     "expand <id> <tool-id>",
			tracksUsage: true,
			run:         func(args []string, reader claudecodec.Codec) { cmdExpand(args, reader) },
		},
		{
			name:        "stats",
			summary:     "字元與 token 分佈統計",
			argHint:     "stats <id>",
			tracksUsage: true,
			run:         func(args []string, reader claudecodec.Codec) { cmdStats(args, reader) },
		},
		{
			name:        "audit",
			summary:     "檢視被過濾的內容取樣",
			argHint:     "audit <id>",
			tracksUsage: true,
			run:         func(args []string, reader claudecodec.Codec) { cmdAudit(args, reader) },
		},
		{
			name:    "usage",
			summary: "CLI 使用紀錄",
			argHint: "usage",
			run:     func(args []string, _ claudecodec.Codec) { cmdUsage(args) },
		},
		{
			name:    "help",
			summary: "顯示子命令速查表",
			// No argHint: help is a meta/discovery command, not part of the
			// skill's quick-launch surface (see buildArgumentHint in help_cmd.go).
			run: func(args []string, _ claudecodec.Codec) { cmdHelp(args) },
		},
		{
			name:        "benchmark",
			summary:     "掃描近期 session，計算壓縮率與成本比較",
			tracksUsage: true,
			// No argHint: benchmark is a maintainer/analysis command, not part of
			// the skill's quick-launch surface.
			run: func(args []string, reader claudecodec.Codec) { cmdBenchmark(args, reader) },
		},
		{
			name:        "inject",
			hidden:      true,
			tracksUsage: true,
			run: func(args []string, reader claudecodec.Codec) {
				fmt.Fprintln(os.Stderr, "cc-session inject 已改名為 cc-session inherit，inject 別名將於未來版本移除，請改用 inherit。")
				cmdInherit(args, reader)
			},
		},
	}
}

// findCommand looks up a command by its exact name, including hidden ones —
// hidden only affects display surfaces, not dispatch.
func findCommand(name string) (command, bool) {
	for _, cmd := range commands {
		if cmd.name == name {
			return cmd, true
		}
	}
	return command{}, false
}
