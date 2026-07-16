# ADR-005: Collapse Retry Loops and Consecutive Same-File Reads

## Status

Accepted

## Context

Real transcripts contain two high-frequency repetition patterns that each occupy one summary line per call while carrying near-identical information:

- **Retry loops**: the model retries a failing command with minor variations (a real sample: 11 consecutive failed `git worktree` diagnostics, all producing the same `Path ... does not exist` error).
- **Chunked reads**: the model reads a large file in several offset windows (`Read` of the same path 2–4 times in a row).

ADR-001 established the precedent for collapsing adjacent redundant lines (consecutive cc-session calls) and its verbose-flag exemption. This ADR extends collapsing to these two patterns. Like all output-format decisions, the collapsed lines are consumed by LLMs reading injected context, so the rules below are a contract.

## Decision

### Rule 1 — retry-loop collapse

Consecutive tool calls collapse into one line when **all** hold:
- same tool name,
- every call FAILED,
- normalized first-line commands are identical or one is a non-empty prefix of the other (tail-argument variations count as the same retry attempt; two commands where neither prefixes the other never merge).

Rendered as `[Bash#<last-id>] <description> -> FAILED ×N: <last error excerpt>`. The **last** call's tool id and error are kept: the final state is what the retry loop converged to, and `expand` on that id reaches it.

Never collapses across: an interleaved success, a different tool, any non-tool event (assistant text may reference individual results), or when `-verbose-bash` is set (ADR-001 exemption pattern).

### Rule 2 — same-file Read collapse

Consecutive successful `Read` calls of the same `file_path` (offsets/limits may differ) collapse to `[Read#<last-id> ×N] <path> -> ok`. Any interleaved event or failure breaks the group.

### Invariants

- **Failure information is never lost to collapsing** — only repetition of the *same* failure is compressed; distinct failures stay on their own lines.
- `×1` never appears; a single call renders unchanged.
- Comparison is deliberately conservative: prefer under-collapsing to mis-collapsing. Widening the identity rule requires a real transcript sample demonstrating the missed pattern, plus a regression test (same evidence bar as ADR-003's sniff list).

## Consequences

- The 11-line retry sample renders as 6 lines (distinct commands preserved, three retry groups at ×2/×3/×4).
- `analyzer` stats stay reconciled because KEPT categories are measured from the render sink *after* collapsing (ADR-003 follow-up work); the reconciliation regression test guards this.
- Negative knowledge: collapse identity uses the command's first line, not the full input — multi-line Bash scripts with identical first lines but different bodies will merge if all fail; accepted because the excerpt shown is the last attempt's real error, and expand reaches every collapsed id via the transcript.
