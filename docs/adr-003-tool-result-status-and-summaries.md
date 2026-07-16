# ADR-003: Tool Result Status Determination & Error/Diff Summaries

## Status

Accepted

## Context

cc-session renders every tool result with a status token (`-> ok` / `-> FAILED`). This status is consumed by LLMs reading injected context, so a wrong status is worse than a missing detail: the reader concludes a step succeeded when it actually failed.

The current determination (`internal/claudecodec/model.go`) reads only `toolUseResult.success` and **defaults to true when the field is absent**. Inspection of real Claude Code JSONL transcripts shows this default is wrong for the majority of tool results:

- **Bash** results carry `toolUseResult: {stdout, stderr, interrupted, isImage, noOutputExpected}` — there is **no `success` field**. Failed commands (non-zero exit, hook rejections, `no such file or directory`) are therefore rendered as `-> ok: Exit code 1`, a self-contradictory line.
- **Read** results (`{type: "text", file: {...}}`) also carry no `success` field.
- Only a minority of tools (e.g. Agent lifecycle operations) set `success` explicitly.
- The `is_error` flag on the `tool_result` content block is **not parsed by the codec at all** — and even it is unreliable: real transcripts contain `is_error: false` on Bash results whose text says `Exit code 1`. Claude Code reports non-zero exits as normal results with the exit code appended to the text.

Separately, ADR-002's "Next Phase Optimizations" proposed diff summaries for Edit/Write and smarter Bash failure excerpts. Real transcripts confirm Edit results carry `toolUseResult.structuredPatch` as a list of hunks `{oldStart, oldLines, newStart, newLines, lines}` (where `lines` entries are prefixed `+`/`-`/` `), so a one-line diff summary is computable without an LLM. Write results for new files carry `structuredPatch: []` plus `content`.

## Decision

### 1. Status determination ladder

Determine a tool result's status by the first applicable rule:

1. `toolUseResult.success` present → use it (explicit signal wins).
2. `tool_result` content block has `is_error: true` → failed. (The codec must start parsing this field.)
3. Content sniffing on the result text, using a **conservative, enumerated** pattern list — currently:
   - a line matching `Exit code N` with N ≠ 0 (Bash convention),
   - a `... hook error` prefix (PreToolUse/PostToolUse hook rejections).
4. Otherwise → ok.

Sniffing is deliberately a small allowlist of known-failure signatures, not a heuristic: a false `FAILED` misleads the reader just as badly as a false `ok`. New signatures are added only with a real transcript sample as evidence, and each gets a regression test.

### 2. Error excerpts

On failure, the summary keeps the first **meaningful** error line instead of blindly the first line:

- Skip known noise prefixes when picking the excerpt: `cat -n` style line numbers (`^\s*\d+\t`), hook boilerplate, and the bare `Exit code N` line itself (the code is already reflected in the status; per ADR-002, surface the actual compiler/test error beneath it).
- Failed results get a larger excerpt budget than successful ones (single line, up to ~200 chars) — errors are the content least safe to drop.

The status token stays `ok` / `FAILED` (unchanged) to avoid churning the existing output contract.

### 3. Diff summaries for Edit/Write

Successful Edit/Write results upgrade from a bare `-> ok`:

- **Edit** with non-empty `structuredPatch`: `-> ok (+A, -D @ L<newStart>)`, where A/D are `+`/`-` line counts summed across hunks and L is the first hunk's start; append `, H hunks` when H > 1.
- **Write** (new file, empty `structuredPatch`): `-> ok (new file, N lines)` from `content`.
- `structuredPatch` missing or unparsable: fall back to the current `-> ok`.

### 4. Unknown-tool fallback

The summarizer's default branch currently renders `[ToolName]` with zero input context. It changes to render the first 2–3 input key/value pairs (each value truncated to ~60 chars), so tools added to Claude Code after this release degrade gracefully instead of silently losing all information.

## Negative knowledge (do not "simplify" these away)

- `toolUseResult.success` is **absent** for the highest-frequency tools (Bash, Read). Defaulting the absent case to either value is a guess; the ladder above exists because no single field is authoritative.
- `is_error` can be `false` on genuinely failed Bash commands. It is a failure signal when true, never a success signal when false.
- Content sniffing therefore cannot be removed in favor of "just use the flags" — the flags do not carry the information.

## Consequences

- Injected context becomes trustworthy about which steps failed — the primary correctness property of this tool.
- Diff summaries add a few tokens per Edit/Write but answer the most common follow-up question ("what did that session actually change") without `expand`.
- New failure shapes default to `ok` until their signature is added to the sniff list; the audit command remains the mechanism for spotting such gaps.
