# ADR-004: Failed Tool Result Retention Strategy

## Status

Proposed — implementation deliberately deferred. Current behavior (ADR-003: single meaningful error line, ~200 runes) stays in place. This document records the investigation, fleet data, and adversarial review so a future revision starts from evidence instead of re-deriving it.

## Context

ADR-003 fixed failure *detection* (status ladder) and widened the failure excerpt to one meaningful line / 200 runes. The open question was retention: errors are the content least safe to drop, yet multi-line errors (compiler output, tracebacks) lose everything past the first meaningful line. Truncation has a hidden cost — if the excerpt is insufficient to judge the root cause, the reading LLM calls `expand`, paying excerpt + tool round-trip + full text, which is worse than full retention. Telemetry forensics found a real instance: one session burst-called `expand` 19 times in 6.5 minutes after inheriting its own history (self-recovery after context compaction) — when a summary is insufficient, the failure mode is exploratory fumbling, not one clean retrieval.

### Fleet data (379 sessions, 30 days, 1,194 failures per the ADR-003 ladder)

Length distribution of failed-result text (chars):

| p50 | p75 | p90 | p95 | p99 | max |
|-----|-----|-----|-----|-----|-----|
| 143 | 407 | 1,274 | 2,093 | 11,895 | 26,029 |

- Extremely right-skewed: half of failures fit in 143 chars; the longest 10% (>1,274) hold **68%** of total failure chars (522K/763K). The long tail is compiler dumps and full test-suite output.
- Failures are rare per session: median 1, p90 7; 30% of sessions have none.
- Candidate thresholds for "short errors fully retained": p75=407 → 75% coverage; p90=1,274 → 90%; p95=2,093 → 95%. **~1,500 chars** (91-92% coverage) was the working recommendation.

Known bias: the ladder is deliberately conservative (unknown shapes → ok), so this distribution describes *detected* failures only; the true distribution can only be wider. Also measured in chars/runes, not tokens.

### Design conclusions that survived adversarial review

A six-claim design was adversarially reviewed by a second model (Codex, xhigh); four PARTIAL, two REFUTED. What survived, with corrections applied:

1. **Two-tier retention**: failures ≤ ~1,500 chars fully retained; longer ones truncated with head + tail + signal lines. Any reasonable threshold strictly beats the status quo (200 runes flat), so "not yet optimal" is not a reason to keep the worst variant — but see Decision for why we still defer.
2. **Global ranking, not stacked layers** (review correction): candidate lines (head, tail, keyword-matched signal lines) compete in one weighted ranking with per-category caps and a budget — not sequential layers where earlier ones exhaust the budget.
3. **Signal lines**: lines matching `Error:|panic:|FAILED|Caused by:|Traceback|assert` — root-cause position is ecosystem-dependent (Python/Java/test-runners: tail; Go/Node/compilers: head), so head+tail+signals covers both. *Unproven*: the head/middle/tail optimal ratio was never measured against annotated samples; ship conservative, iterate via audit.
4. **Self-describing omission markers**: `(... N lines omitted — <kind>; full text: cc-session expand <session> <tool-id>)`. The expand command must be injected by the formatter (Summary() has no session ID) and must be executable as printed (short-ID collision → print a longer ID). The marker must be honest about what was kept vs omitted per category.
5. **Retention budget should follow re-derivability, not success/failure** (review correction to the original claim): Read output is re-derivable (the file is on disk; re-reading yields the *current* version, usually better); Bash stdout — success or failure — is historical evidence (test logs, API responses, transient state) recoverable only via `expand`. Success/failure is a coarse proxy; the real axis is whether the reader can reconstruct the content.
6. **Feedback loop**: `inherit → expand` adjacency in usage.jsonl is a weak proxy for truncation failure. To make it usable, `expand` must log the tool-id argument (currently only the session id is recorded), and logging coverage must be measured first (entries are silently skipped when no caller is detected).

### Project-path rescue: investigated and rejected

Proposal: preserve error lines referencing project-owned files (Sentry-style "in-app frames"), on the actionability argument that the reader — a new Claude session in the same project — can act on `src/foo.go:42` (Read it) but cannot re-run the historical failure.

Fleet measurement (378 transcripts, 1,169 failures) refuted the premise that such lines hide in the middle where head+tail misses them:

- Path-line positions in long failures are **uniform** (head/middle/tail = 31.5%/35.5%/33.0%) — head+tail alone already captures ~2/3.
- Long failures where path lines exist *only* outside a head+tail window: 5–11 of 94 (0.4–0.9% of all failures).
- Of those rescued middle lines, over half are noise: node_modules stack frames carrying the project prefix, pnpm script banners. Only 1 of 5 sampled was a true signal (`thread 'main' panicked at src/db.rs:401:57:` mid-way through cargo test output).
- Path-line counts have a long tail (p90=27 per long failure) — any rescue layer would need per-result caps.

Verdict: a dedicated path-matching layer costs a subsystem (project-root resolution, interaction with `cleanCwdPaths` which rewrites cwd to `.`, path-boundary/symlink handling, `ToolResult` has no cwd field) and buys <1% coverage, half noise. A narrowed `path:line` regex in the signal set was considered and rejected as a workaround. If revisited, the value concentrates in "relative path + line number" patterns (panic/traceback style) with node_modules excluded.

## Decision

**Defer implementation. Keep ADR-003 behavior unchanged.**

Rationale: the two-tier design is well-evidenced but its supporting instrumentation is not in place — the feedback loop (expand tool-id logging, coverage measurement) is what turns a one-shot threshold guess into a tunable system, and shipping retention changes without it means flying blind on exactly the metric (truncation-forced expands) the design is meant to minimize. The investigation cost is sunk into this document; the implementation can start from here at any time.

### Revisit triggers

- Recurring burst-expand episodes (the 19-in-6.5-min pattern) observed in telemetry after inherit.
- `expand` starts logging tool ids (prerequisite for the feedback loop).
- A concrete user report of a failure whose root cause was invisible in the one-line excerpt.

### Known limitations to carry into any implementation

- False-negative bias in the failure statistics (conservative ladder).
- Runes ≠ tokens; threshold should eventually be validated against token counts.
- Content types (compiler diagnostics vs stack traces vs assertions) may deserve distinct strategies; the generic signal regex can keep a wrong line and drop the adjacent key line.
- Fully-retained short errors can expose secrets/env vars/API responses; consider redaction.
