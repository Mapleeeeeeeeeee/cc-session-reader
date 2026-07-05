# ADR-002: Tool Result Compression Optimization & Project-Relative Paths

## Status

Proposed

## Context

In the current implementation of `cc-session-reader`, the CLI compresses tool results by taking the first line of the output text. However, for core tools such as `Read`, `Write`, `Edit`, and `Agent`, this general policy yields noisy and meaningless information:

- **`Read`**: Displays the first line of the read file (e.g. `import ...` or `'use client'`), which is irrelevant since the fact that it read successfully is already implied, and the file name is already known.
- **`Write` / `Edit`**: Displays boilerplate confirmations containing local absolute paths (e.g. `The file /Users/maple/... has been updated successfully.`), which is redundant and leaks local environment details.
- **`Agent`**: Displays launch confirmations (e.g. `Async agent launched successfully.`), which only confirms the subagent process was spawned, rather than its actual output (which is already displayed in teammate messages).

Additionally, the `ToolUse` representation only shows the base name of the files for `Write` and `Edit` (e.g., `mutations.ts`), causing ambiguity when multiple files with the same name exist in different directories.

## Decision

We optimize the compression format of these tools and introduce project-relative paths by extracting the `cwd` field from the raw JSONL events.

### 1. Project-Relative Path Resolution
- Add a `cwd` field to the parser and domain events.
- For `Read`, `Write`, and `Edit` tools, resolve the absolute target file paths relative to the project root (`cwd`) using `filepath.Rel`.
- Fall back to clean paths or home-relative paths (`~/...`) if the file is outside the project root.

### 2. Tool-Specific Compression Formats

#### Read Tool
- Format `ToolUse` as `[Read] path/to/file:offset:limit` (e.g., `api/mutations.ts:21:120`) if line range parameters are present in the input.
- Format `ToolResult` on success as a clean `-> ok` (avoiding redundant line counts in the suffix).

#### Write & Edit Tools
- **Option A (Minimal)**: Compress success results to a simple `-> ok` (e.g. `[Edit] src/routes/v1/courses.ts -> ok`). This keeps the timeline extremely clean, leaving details to the assistant's subsequent explanation or to `expand` inspection.
- For failed operations (`Success == false`), the raw error message is always preserved.

#### Agent Tool
- Compress successful launch results to `-> ok`.
- Keep the actual subagent report in the teammate message unmodified.

## Next Phase Optimizations (Future Work)

In the next phase of tool compression, we will explore generating **meaningful summaries** of code changes instead of a minimal `-> ok`:
- Parse `structuredPatch` in the raw JSONL to count added/deleted lines and locations (e.g. `-> ok (+15, -3 lines at L20)`).
- Extract code change context (such as function/class names or a tiny diff preview) statically without using LLMs.
- Optimize Bash failure messages by skipping the boilerplate `Exit code 1` line and finding the actual compiler or test error message.

## Consequences

- The timeline output becomes cleaner and more readable, saving context token usage.
- Duplicate filename ambiguity is solved via project-relative path prefixes.
- Local environment absolute paths are removed from the outputs.
- Future work: Execute benchmarks to verify token savings.
