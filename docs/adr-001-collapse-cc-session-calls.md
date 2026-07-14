# ADR-001: Collapse cc-session tool calls in rendered output

## Status

Accepted

## Context

When a Claude Code session uses `cc-session inherit` (formerly `cc-session inject`, see the CLI rename) to load another session's content, the JSONL records each page as a separate Bash tool call. A 4-page inherit produces 4 lines like:

```
[Bash#Y1dg] cc-session inherit 16d06326-... -> ok: [page 1/4 | lines 1-377 of 1320]
[Bash#Ybwi] cc-session inherit 16d06326-... -> ok: [page 2/4 | lines 378-720 of 1320]
[Bash#iNMY] cc-session inherit 16d06326-... -> ok: [page 3/4 | lines 721-1051 of 1320]
[Bash#iMqP] cc-session inherit 16d06326-... -> ok: [page 4/4 | lines 1052-1320 of 1320]
```

These lines carry zero information for the reader — the injected content was consumed by the AI in that session, and its conclusions already appear in the subsequent assistant messages. Showing 4 redundant lines wastes output space and context tokens when the output is fed back into another session.

## Decision

Collapse consecutive `cc-session inherit/read/context` Bash tool calls targeting the same session into a single descriptive line:

```
(cc-session: inherited session 16d06326 here, 1320 lines omitted)
```

The collapsing is:
- **Automatic** — no flag required. The original tool call details remain accessible via `cc-session expand <session> <tool-id>`.
- **Scoped to cc-session CLI calls only** — other Bash tool calls (including the model fumbling with `which cc-session`, `node cc-session.mjs`, etc.) render normally as one-line summaries.
- **Applied in both `read` and `context` output formats** — the collapsing logic lives in a shared `collapseCCSessionTools()` function called by both renderers' flush paths.
- **Backward compatible with `inject`** — older transcripts recorded before the `inject` → `inherit` CLI rename still contain literal `cc-session inject` calls. Those still collapse the same way, keeping their historical "injected" wording so old sessions read naturally.

### Detection

`parseCCSessionCommand(cmd)` checks if a Bash command matches `cc-session {inherit|inject|read|context} <session-id>`. It returns the subcommand and an 8-char session ID prefix. Args starting with `-` (flags like `-h`) are rejected to avoid false positives.

### Total lines extraction

`parseTotalLines(text)` extracts the total line count from the first line of a tool result, matching the `of N]` pattern in cc-session's page markers. Only the first line is searched to avoid false matches against session content that contains "of " elsewhere.

### Verb selection

- `inherit` → "inherited session X here"
- `inject` (legacy) → "injected session X here"
- `read` / `context` → "loaded session X here"

## Consequences

- Readers see a clean one-liner instead of N repetitive tool call summaries
- The injected session's actual content is not shown (it was already summarized by the AI in the original session)
- `cc-session expand` still works for inspecting individual tool calls when needed
- Future work: a `--follow-refs` flag could inline or re-render the referenced session's content, but that requires resolving the referenced session's JSONL (a different architectural approach)
