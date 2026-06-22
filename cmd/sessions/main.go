// Package main is the CLI entry point for the Claude session reader.
// Subcommands: list, read, context, stats, audit, expand, usage, inject.
package main

import (
	"fmt"
	"os"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/tokens"
)

// countTokensFn is the token-counting backend used by runStats. It is a
// package-level seam so tests can substitute a deterministic offline stub
// (success or failure) without making real Anthropic API calls.
var countTokensFn = tokens.CountTokensAPI

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	defer waitUsageLog()

	reader := claudecodec.Codec{}

	subcommand := os.Args[1]
	switch subcommand {
	case "-h", "--help", "help":
		printUsage()
		return
	case "list":
		cmdList(os.Args[2:], reader)
	case "read":
		cmdRead(os.Args[2:], reader)
	case "context":
		cmdContext(os.Args[2:], reader)
	case "stats":
		cmdStats(os.Args[2:], reader)
	case "audit":
		cmdAudit(os.Args[2:], reader)
	case "expand":
		cmdExpand(os.Args[2:], reader)
	case "usage":
		cmdUsage(os.Args[2:])
	case "inject":
		cmdInject(os.Args[2:], reader)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", subcommand)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: cc-session <command> [options]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  list      列出最近的 session")
	fmt.Fprintln(os.Stderr, "  read      完整對話 + tool call 一行摘要")
	fmt.Fprintln(os.Stderr, "  context   精簡注入格式（帶 metadata header）")
	fmt.Fprintln(os.Stderr, "  stats     字元與 token 分佈統計")
	fmt.Fprintln(os.Stderr, "  audit     檢視被過濾的內容取樣")
	fmt.Fprintln(os.Stderr, "  expand    展開特定 tool call 完整內容")
	fmt.Fprintln(os.Stderr, "  usage     CLI 使用紀錄")
	fmt.Fprintln(os.Stderr, "  inject    分頁注入 session 到 context")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Run 'cc-session <command> -h' for command-specific flags.")
}
