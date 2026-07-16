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
- normalized first-line commands are identical or one is a prefix of the other **ending on a token boundary and containing more than one token** (the next character after the prefix must be whitespace, and a bare program name never anchors a match — `git add` prefixes `git add --intent-to-add`, but not `git add-on-something`, and a bare `git` does not absorb `git add`; a whitespace boundary alone would wrongly allow the latter),
- **the error excerpts are identical** (same first meaningful error line) — distinct errors never merge, even under the same command.

Rendered as `[Bash#<last-id>] <description> -> FAILED ×N: <last error excerpt>`. The **last** call's tool id is kept: the final state is what the retry loop converged to, and `expand` on that id reaches it. (Since the error-equality condition holds, the shown excerpt is every collapsed call's error, not just the last one's.)

Never collapses across: an interleaved success, a different tool, any **rendered** event (assistant text may reference individual results), or when `-verbose-bash` is set (ADR-001 exemption pattern). Non-rendered events (harness noise, system reminders, non-verbose thinking) do not break a group — the reader cannot reference what the output never shows.

### Rule 2 — same-file Read collapse

Consecutive successful `Read` calls of the same `file_path` (offsets/limits may differ) collapse to `[Read#<last-id> ×N] <path> -> ok`. Any interleaved event or failure breaks the group.

### Invariants

- **Failure information is never lost to collapsing** — enforced by the error-equality condition: only repetition of the *same* failure is compressed; distinct failures stay on their own lines. (The first shipped version lacked this condition and could hide an earlier, different error behind the last one — caught by adversarial review before any release.)
- `×1` never appears; a single call renders unchanged.
- Comparison is deliberately conservative: prefer under-collapsing to mis-collapsing. Widening the identity rule requires a real transcript sample demonstrating the missed pattern, plus a regression test (same evidence bar as ADR-003's sniff list).

## Consequences

- The 11-line retry sample renders as 6 lines (distinct commands preserved, three retry groups at ×2/×3/×4 — all sharing the same error, so the error-equality condition keeps them collapsed).
- `analyzer` stats stay reconciled because KEPT categories are measured from the render sink *after* collapsing (ADR-003 follow-up work); the reconciliation regression test guards this.
- Negative knowledge (accepted, documented limits):
  - Collapse identity uses the command's **first line**, not the full input — multi-line Bash scripts with identical first lines and identical error excerpts still merge; acceptable because error equality bounds the information loss and expand reaches every collapsed id via the transcript.
  - Whitespace normalization is not shell-aware: `strings.Fields` folds whitespace inside quoted arguments, so commands differing only in quoted internal spacing can share a signature. With the error-equality condition the residual mis-merge risk is negligible; revisit only with a real sample.
  - Read grouping keys on the raw `file_path` string, not a canonical path — same relative path under different cwds would merge, and symlink/`..`/absolute-vs-relative spellings of one file won't. Both accepted: cwd changes within one render batch are rare, and under-merging is the preferred failure direction.
