# Benchmark: cc-session Cost Savings

Compares the input token cost of two scenarios after the Claude API prompt cache expires (5-minute TTL):

- **Scenario A**: Stay in the original session. The entire context is re-cached on the first API call.
- **Scenario B**: Open a new session, inject compressed history via cc-session, then continue working.

## Quick Start

### 1. Measure your overhead (once)

Open a fresh Claude Code session, type something short (e.g. "hi"), then:

```bash
cc-session stats <session-id>
```

Note the `Last turn context` value. This is your session overhead (system prompt + tool definitions + CLAUDE.md + rules). It varies per user.

### 2. Run the benchmark

The benchmark uses Anthropic's token counting API for the compressed history,
so `ANTHROPIC_API_KEY` or `anthropic_api_key_file` must be configured.

```bash
cc-session benchmark --overhead <your-number>
```

All other parameters are derived automatically from your real session data.

### Flags

| Flag | Default | Description |
|------|:-------:|-------------|
| `--overhead` | 40000 | Your measured session overhead tokens |
| `--days` | 30 | How far back to scan for sessions |
| `--min-kb` | 100 | Minimum JSONL file size in KB |
| `--n` | 10 | Max successful session results to report |
| `--model` | opus | Pricing and token-counting model: `opus`, `opus-4-6`, `opus-4-7`, `opus-4-8`, or `sonnet` |

### Example output

```
=== Compression ===
Session        Context      NewCtx   Saved
e61060b1       403,129     125,089  69.0%
977b7360       381,032      76,808  79.8%

Median: 74.4%   Mean: 74.4%   Range: 69.0% — 79.8%

=== Cost Savings Per Session (opus) ===
Session        Context      NewCtx      K  Break-even   10-turn   100-turn
e61060b1       403,129     125,089    4.5      turn 1       66%        52%
977b7360       381,032      76,808    5.5      turn 1       73%        45%

Median break-even: turn 1 | 10-turn saving: 69% | 100-turn saving: 48%
```

## Cost Model

### Pricing

From [Anthropic prompt caching docs](https://platform.claude.com/docs/en/build-with-claude/prompt-caching):

| Bucket | API field | Rate (Opus) |
|--------|-----------|:-----------:|
| Cache read | `cache_read_input_tokens` | $0.50/M (0.1× base) |
| Cache write | `cache_creation_input_tokens` | $6.25/M (1.25× base) |
| Uncached input | `input_tokens` | $5.00/M (1× base) |
| Output | `output_tokens` | $25/M (excluded — same for both scenarios) |

### Per-API-call billing

From the [same source](https://platform.claude.com/docs/en/build-with-claude/prompt-caching), "Automatic caching" table:

Each API request's input tokens split into three buckets:
- **cache read**: prefix matching previous cache
- **cache write**: new content written to cache (up to auto-placed breakpoint)
- **uncached**: content after last breakpoint (near zero with auto-caching)

### Multi-turn cache behavior

From the [Automatic caching table](https://platform.claude.com/docs/en/build-with-claude/prompt-caching):

| Request | Cache behavior |
|---------|---------------|
| Request 1 | Everything written to cache |
| Request N | Reads [system..User(N-1)] from cache; writes [Asst(N-1) + User(N)] to cache |

Cross-turn cache write = previous response (R) + new prompt (P).

### Tool call caching

From [tool-use-with-prompt-caching](https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-use-with-prompt-caching):

- Client-side tools (bash/read/edit) are separate API calls with independent cache accounting
- Server-side tools get automatic breakpoints (not applicable to Claude Code's client tools)

Each user turn with K tool calls = K separate API requests, each paying cache read on the full prefix.
When `K` is fractional, benchmark interpolation uses all full extra calls plus
a proportional fractional next call. It does not round `K` to an integer.

### Formulas

**Scenario A** (cache expired, stay in session):

```
Turn 1:
  Call 1: (X + P) × CacheWrite              ← cache miss, write everything
  Calls 2..K: prefix × CachedRead + s × CacheWrite  ← tool loop

Turn N (N≥2):
  prefixFromPrev = (X + P) + (N-1)×toolIOPerTurn + (N-2)×growth
  Call 1: prefixFromPrev × CachedRead + growth × CacheWrite
  Calls 2..K: (prefixFromPrev + growth + s×(c-2)) × CachedRead + s × CacheWrite
```

**Scenario B** (new session + cc-session):

```
Setup:
  If injectPages <= 1:
    (overhead + C) × CacheWrite

  If injectPages > 1:
    overhead × CacheWrite
    For each inject page i:
      (overhead + tokensFromPriorInjectedPages) × CachedRead
      + pageTokens × CacheWrite

  where pageTokens ≈ C / injectPages

Turn 1:
  Call 1: base × CachedRead + P × CacheWrite
  Calls 2..K: (base + P + s×(c-2)) × CachedRead + s × CacheWrite

Turn N (N≥2): same as A but with smaller base
```

### Parameters

| Parameter | Source | Description |
|-----------|--------|-------------|
| X | `stats.LastContextTokens` | Original session context size |
| C | Anthropic token counting API on `filteredText`, using the selected `--model` family | cc-session compressed history size |
| overhead | `--overhead` flag (user-measured) | System + tools + CLAUDE.md |
| NewCtx | `overhead + C` | New session total context after injecting cc-session output |
| injectPages | `inject.SplitPages(inject.RenderFullOutput(...))` | Number of `cc-session inject` pages needed to load the compressed history |
| K | `APICallCount / UserTurnCount` | API calls per user turn |
| toolIOPerCall (s) | `Σ(PerTool.InputChars + ResultChars) / Σ(CallCount) / 2` | Avg tool I/O per API call |
| avgResponse | `TotalOutputTokens / APICallCount` | Avg output tokens per API call |
| prompt (P) | `(LastContext - overhead) / turns - avgResp - toolIO×(K-1)` | Avg user prompt per turn |
| growth | `avgResponse + prompt` | Cross-turn cache write (R + P) |

The compression table compares total context to total context: `X` vs `NewCtx`.
Cost simulation still keeps `C`, `overhead`, and `injectPages` separate because
`NewCtx` is the final context size after injection, not necessarily the cost of
a single request. `cc-session inject` is paginated at ≤20K bytes per page, and
each page is modeled as its own API request: the request reads the prefix loaded
so far and writes the next page. For a one-page injection, the setup model keeps
the historical one-shot `NewCtx × CacheWrite` behavior.

`X` comes from transcript API usage and `C` comes from the Anthropic token
counting API. The `--model` flag controls both pricing and the tokenizer used by
the token counting API. `opus` is an alias for `opus-4-8`; explicit Opus versions
map to `claude-opus-4-6`, `claude-opus-4-7`, or `claude-opus-4-8`; `sonnet` maps
to `claude-sonnet-4-6`. Opus 4.6, 4.7, and 4.8 use the same Opus pricing rates.
Fallback constants are used only for behavior that cannot be read directly from
transcript usage, such as sparse tool I/O data.

### Simplifications

- **Uncached input (`input_tokens`) omitted**: with auto-caching the breakpoint sits on the last cacheable block, making the uncached tail near zero. This is an inference from the docs (not an explicit statement). Impact on results: < 2% based on sensitivity analysis.
- **Output tokens excluded**: identical in both scenarios.
- **Compacted sessions skipped**: sessions with `compact_boundary` events are excluded because the comparison is invalid when the original context was already summarized.

## How It Works Internally

```
┌─────────────────────┐
│  parser.ListAll     │  Scan ~/.claude/projects/*/  and session-meta/
│  Sessions()         │  for JSONL files + metadata
└────────┬────────────┘
         │ filter by --days and --min-kb, sort by size
         ▼
┌─────────────────────┐
│  reader.ReadAll     │  Read JSONL → parse each line into session.Event
│  (path)             │  via claudecodec (handles message types, tool
└────────┬────────────┘  calls, compact boundaries, API usage fields)
         │
         ▼
┌─────────────────────┐
│  analyzer.Compute   │  Walk events, accumulate:
│  Stats(events)      │  - RawText / FilteredText (compression)
└────────┬────────────┘  - LastContextTokens, TotalOutputTokens (from API usage)
         │               - APICallCount, UserTurnCount
         │               - PerTool (InputChars, ResultChars, CallCount)
         │               - CompactCount (for skip detection)
         ▼
┌─────────────────────┐
│  tokens.NewCounter  │  Resolve API key once with the selected token-counting
│  (model)            │  model, then reuse the counter for successful sessions.
└────────┬────────────┘
         │ count FilteredText; skip compacted sessions and sessions without
         │ API usage data until --n successful results have been collected
         ▼
┌─────────────────────┐
│  counter.Count      │  Anthropic token counting API for filteredTokens.
│  (filteredText)     │  LastContextTokens comes from transcript API usage.
└────────┬────────────┘
         │
         ▼
┌─────────────────────┐
│  inject.RenderFull  │  Render the exact text used by `cc-session inject`,
│  + SplitPages       │  then count pages with the same ≤20K byte pagination.
└────────┬────────────┘
         │
         ▼
┌─────────────────────┐
│  Derive per-session │  K, toolIOPerCall, avgResponse, prompt, growth,
│  cost params        │  — all from the stats above
│                     │  plus injectPages from the rendered inject output
└────────┬────────────┘
         │
         ▼
┌─────────────────────┐
│  cumulativeCostA()  │  Simulate N turns × K API calls, sum:
│  costBWithPages()   │  cache_read + cache_write per call
└────────┬────────────┘
         │
         ▼
┌─────────────────────┐
│  Output             │  Compression table + Cost Savings table
└─────────────────────┘
```

### Key data sources

- **Session JSONL** (`~/.claude/projects/<project>/<session-id>.jsonl`): raw transcript of all events (user messages, assistant responses, tool calls, tool results, API usage)
- **Session metadata** (`~/.claude/usage-data/session-meta/<session-id>.json`): lightweight index with project path, timestamps, message counts
- **API usage fields** (embedded in assistant message events in the JSONL): `input_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens`, `output_tokens` — these give us the real `LastContextTokens` and `TotalOutputTokens`
