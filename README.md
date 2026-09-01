# cc-session-reader

[![CI](https://github.com/Mapleeeeeeeeeee/cc-session-reader/actions/workflows/ci.yml/badge.svg)](https://github.com/Mapleeeeeeeeeee/cc-session-reader/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Mapleeeeeeeeeee/cc-session-reader?logo=github)](https://github.com/Mapleeeeeeeeeee/cc-session-reader/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/Mapleeeeeeeeeee/cc-session-reader.svg)](https://pkg.go.dev/github.com/Mapleeeeeeeeeee/cc-session-reader)
[![Go Report Card](https://goreportcard.com/badge/github.com/Mapleeeeeeeeeee/cc-session-reader)](https://goreportcard.com/report/github.com/Mapleeeeeeeeeee/cc-session-reader)
[![Go Version](https://img.shields.io/github/go-mod/go-version/Mapleeeeeeeeeee/cc-session-reader)](go.mod)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey)](https://github.com/Mapleeeeeeeeeee/cc-session-reader/releases)

**English** | [繁體中文](README.zh-TW.md)

A CLI tool that reads Claude Code session transcripts and emits a compact summary.
Every tool call is collapsed into a single line (tool name + key arguments + result status), while conversation text is preserved in full.
Purely static extraction — no LLM involved.

Token reduction depends on what the session is made of: tool-I/O-heavy sessions typically reach **80–88%**;
sessions dominated by large plan documents or plain conversation reduce less (measured around 40–65%), because user/assistant text is kept verbatim and never compressed.

## Installation

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/Mapleeeeeeeeeee/cc-session-reader/main/install.sh | bash
```

The script downloads the binary for your platform into `~/.local/bin/cc-session` (override with the `INSTALL_DIR` environment variable)
and installs the Claude Code Skill by default. Pass `--no-skill` if you don't want the Skill.

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/Mapleeeeeeeeeee/cc-session-reader/main/install.ps1 | iex
```

Installs into `$env:LOCALAPPDATA\cc-session\`; in interactive mode it asks whether to add the directory to PATH and install the Skill.

### Other installation methods

- **Download a binary**: grab the archive for your platform from [GitHub Releases](https://github.com/Mapleeeeeeeeeee/cc-session-reader/releases), extract it, and put it on your PATH.
- **go install**: `go install github.com/Mapleeeeeeeeeee/cc-session-reader/cmd/cc-session@latest` (binary lands in `$GOPATH/bin`).
- **Skill only**: `mkdir -p ~/.claude/skills/cc-session && curl -o ~/.claude/skills/cc-session/SKILL.md https://raw.githubusercontent.com/Mapleeeeeeeeeee/cc-session-reader/main/SKILL.md`

## Subcommands

| Command | Description | Example |
|---------|-------------|---------|
| `list` | Browse recent sessions (those that already used cc-session are marked `[refs]`) | `cc-session list -n 10 -p myproject` |
| `read` | Full conversation with inline tool summaries | `cc-session read <id> -max-lines 200` |
| `context` | Compact injection format, including a session metadata header | `cc-session context <id>` |
| `inherit` | Paginated context inheritance (≤28K chars per page, progress tracked automatically, `-reset` starts over) | `cc-session inherit <id>` |
| `stats` | Character and token distribution plus compression ratio | `cc-session stats <id> -no-tokens` |
| `audit` | Sample the filtered-out content to confirm nothing important was dropped | `cc-session audit <id> -n 10` |
| `expand` | Expand the full input/result of a specific tool call | `cc-session expand <id> uCVa` |
| `usage` | Inspect this CLI's own usage history | `cc-session usage -cmd read` |

Session IDs support prefix matching — the first 8 characters are usually enough. `read` and `context` truncate at 200 lines by default (`-max-lines` adjusts it); when truncated they print the total line count and the suggested offset for the next chunk.

`list` reads from session metadata (`~/.claude/usage-data/session-meta/`), so it shows fewer entries than the transcripts on disk; `read`/`context`/`stats` can access any transcript, not just the ones `list` shows.

### Verbose flags (for read / context)

| Flag | Effect |
|------|--------|
| `-verbose-agents` | Keep Agent subagent results in full (default: one-line summary) |
| `-verbose-bash` | Show full stdout/stderr from the Bash tool (default: summarized) |
| `-verbose-thinking` | Show assistant thinking blocks (hidden by default) |
| `-verbose-commands` | Expand full output of slash/bash commands (default: marker only) |

## Compression logic

Tool calls, Bash output, Agent results, and thinking blocks are collapsed into summaries or one-line markers by default; user/assistant conversation text is preserved in full. When a session contains cc-session inherit/read/context calls, consecutive calls are collapsed into a single line (e.g. `(cc-session#id: inherited session X here, N lines omitted)`); `-verbose-bash` skips this collapsing. `cc-session inject` calls in older sessions (the pre-rename command name) are collapsed the same way, keeping the `injected session X here` wording. Injection types such as Skill injection, teammate warnings, command injection, Context Usage blocks, and system-reminders are compressed further or removed entirely to cut context noise. See [SKILL.md](SKILL.md) for the detailed filtering rules.

## Configuration

> 💡 **Note**: this config file is **optional**. Without it, only the token calculations in `stats` and `benchmark` are affected; reading, filtering, injection, and every other core feature work fine.

If you want token statistics or custom behavior, configure `~/.claude/skills/cc-session/config.json`. You can use `config.json.template` from the repository root as a starting point:

```bash
mkdir -p ~/.claude/skills/cc-session
curl -o ~/.claude/skills/cc-session/config.json \
  https://raw.githubusercontent.com/Mapleeeeeeeeeee/cc-session-reader/main/config.json.template
```

The config file supports these fields:

```json
{
  "anthropic_api_key_file": "~/.config/anthropic/.env",
  "integration_test_session": "<session-id>",
  "no_usage": false
}
```

| Field | Purpose |
|-------|---------|
| `anthropic_api_key_file` | Path to a file containing `ANTHROPIC_API_KEY`, enabling exact token counting |
| `integration_test_session` | Session ID used by local integration tests |
| `no_usage` | Set to `true` to disable CLI usage tracking (nothing written to `usage.jsonl`) |

The environment variable `CC_SESSION_NO_USAGE=1` has the same effect and overrides the config.json setting.

## Architecture

```
cmd/cc-session/       CLI entry point, subcommand routing
internal/
  claudecodec/        JSONL reading, noise filtering, raw→event parsing (TranscriptReader / HeaderScanner interfaces)
  session/            Domain model (Event, ToolUse, ToolResult, ToolInput)
  parser/             Session lookup (find transcript, parse IDs, metadata)
  summarizer/         Tool call → one-line summary
  formatter/          Output formatting (read mode, context mode)
  analyzer/           Stats computation, audit sampling
  tokens/             Anthropic token counting API
  inject/             Paginated injection state management
  tracker/            CLI usage tracking
  jsonutil/           JSON map helper functions
```

`claudecodec` is the only package coupled to the JSONL format; every other package accesses session data through the `TranscriptReader` and `HeaderScanner` interfaces.

## Uninstall

```bash
rm ~/.local/bin/cc-session
rm -rf ~/.claude/skills/cc-session
```

## Contributing

Found a bug or want a feature? Open an issue:
https://github.com/Mapleeeeeeeeeee/cc-session-reader/issues

Pull requests are welcome too.

## License

[Apache License 2.0](LICENSE)
