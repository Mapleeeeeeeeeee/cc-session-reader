package benchmark

// Cost model.
//
// Pricing: https://platform.claude.com/docs/en/build-with-claude/prompt-caching
//
//	cache_read  = 0.1x base  (prefix matching previous cache)
//	cache_write = 1.25x base (new content written to cache)
//	input       = 1.0x base  (after last cache breakpoint — near zero with auto-caching)
//
// Multi-turn behavior (same source, "Automatic caching" table):
//
//	Request N reads [system..User(N-1)] from cache,
//	writes [Asst(N-1) + User(N)] to cache. Cross-turn write = R + P.
//
// Tool caching: https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-use-with-prompt-caching
//
//	Client-side tools (bash/read/edit) don't get automatic server breakpoints.
//	Each client tool round-trip is a separate API call with its own cache accounting.
//
// Thinking tokens lifecycle (source: platform.claude.com/docs/en/api/messages):
//
//	output_tokens includes thinking tokens (output_tokens_details.thinking_tokens
//	provides a decomposition, but output_tokens is the authoritative billing total).
//
//	Turn N:   thinking generated         → output_tokens               @ $25/M   (Opus)
//	Turn N+1: first time as input        → cache_creation_input_tokens @ $6.25/M (Opus)
//	Turn N+2: subsequent turns as input  → cache_read_input_tokens     @ $0.50/M (Opus)
//
//	Empirically verified: thinking blocks from previous turns ARE counted as
//	input_tokens in subsequent API calls (Sonnet 4.6, 2026-06-24). The token
//	counting docs say "ignored", but the Messages API bills them — the prompt
//	caching docs ("DO count as input tokens when read from cache") are correct.
//
//	avgResponse (TotalOutputTokens / APICallCount) already includes thinking,
//	so the growth model accounts for thinking tokens accumulating in context.
//
// Per-session derived values (from real session data):
//
//	K (callsPerTurn):  APICallCount / UserTurnCount
//	toolIOPerCall:     Σ(PerTool.InputChars + ResultChars) / Σ(CallCount) / 2 chars-per-token
//	                   (empirically ~1.86 chars/token weighted average; only char counts
//	                   available — raw tool text not stored — so API call not applicable here)
//	avgResponse:       TotalOutputTokens / APICallCount (includes thinking tokens)
//
// Assumptions (not derivable from session data):
//
//	sessionOverhead:  system prompt + tool definitions + CLAUDE.md + rules.
//	                  Varies per user. Measure: open a 1-turn session, check context tokens.
//	                  Pass via --overhead flag; falls back to DefaultOverhead.
//	perTurnPrompt:    average user prompt size (same for both scenarios)
const (
	DefaultOverhead = 40000 // conservative default; use --overhead for your actual value
	PerTurnPrompt   = 10000 // assumption: same for A and B, cancels in comparison
	PerTurnResponse = 2000  // fallback when TotalOutputTokens unavailable
	PerCallToolIO   = 3000  // fallback when PerTool data unavailable
	// empirically measured ~1.86 chars/token weighted avg across 16 content types
	CharsPerToken = 2
)

// CostParams bundles per-session derived values for cost functions.
type CostParams struct {
	K             float64 // API calls per user turn (derived: APICallCount / UserTurnCount)
	ToolIOPerCall int     // tokens per intra-turn API call (derived: PerTool chars / calls / 2)
	AvgResponse   int     // avg output tokens per API call (derived: TotalOutputTokens / APICallCount)
	Prompt        int     // avg user prompt tokens per turn (derived from context growth)
	Growth        int     // cross-turn cache write = avgResponse + prompt
	Overhead      int     // session overhead: system + tools + CLAUDE.md (from --overhead flag)
}

// NewCostParams builds a CostParams from a Result and overhead token count.
func NewCostParams(r Result, overheadTokens int) CostParams {
	g := r.AvgResponse + r.Prompt
	return CostParams{
		K:             r.CallsPerTurn,
		ToolIOPerCall: r.ToolIOPerCall,
		AvgResponse:   r.AvgResponse,
		Prompt:        r.Prompt,
		Growth:        g,
		Overhead:      overheadTokens,
	}
}

// CumulativeCostA models staying in an existing session after cache expires.
//
// Turn 1 (cache expired):
//
//	Call 1: cache write (X + P) — entire context is cache miss
//	Calls 2..K: cache read (growing prefix) + cache write (tool I/O)
//
// Turn N (N>=2):
//
//	Call 1: cache read (prefix from prev turn) + cache write (R + P)
//	Calls 2..K: cache read (growing prefix) + cache write (tool I/O)
//
// Prefix grows across turns by growth + (K-1)*toolIO per turn, because tool I/O
// from previous turns stays in the conversation history.
func CumulativeCostA(turns int, x int, sp CostParams, p Pricing) float64 {
	total := 0.0
	extraCalls := extraCallsPerTurn(sp.K)
	s := sp.ToolIOPerCall
	g := sp.Growth
	toolIOPerTurn := float64(s) * extraCalls
	for n := 1; n <= turns; n++ {
		if n == 1 {
			total += float64(x+sp.Prompt) * p.CacheWrite / 1e6
			total += intraTurnToolCost(float64(x+sp.Prompt), extraCalls, s, p)
		} else {
			prefixFromPrev := float64(x+sp.Prompt) + float64(n-1)*toolIOPerTurn + float64(n-2)*float64(g)
			total += prefixFromPrev*p.CachedRead/1e6 + float64(g)*p.CacheWrite/1e6
			total += intraTurnToolCost(prefixFromPrev+float64(g), extraCalls, s, p)
		}
	}
	return total
}

// CumulativeCostAWarm models staying in an existing session when cache is still warm.
//
// Cache TTL source: https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching
// Subscription users get a 1-hour cache TTL automatically. When you are continuously
// working within that window, the entire prefix X is already cached — Turn 1 behaves
// like Turn N≥2 in the cold model (cache read, not cache write).
//
// Turn N (all N, including N=1):
//
//	Call 1: cache read (prefix from previous turn, or X when N=1) + cache write (R + P)
//	Calls 2..K: cache read (growing prefix) + cache write (tool I/O)
func CumulativeCostAWarm(turns int, x int, sp CostParams, p Pricing) float64 {
	total := 0.0
	extraCalls := extraCallsPerTurn(sp.K)
	s := sp.ToolIOPerCall
	g := sp.Growth
	toolIOPerTurn := float64(s) * extraCalls
	for n := 1; n <= turns; n++ {
		// n=1: prefixFromPrev = X (fully cached); growth shifts by 1 vs cold because
		// the previous turn's R is already in cache before our counting starts.
		prefixFromPrev := float64(x) + float64(n-1)*toolIOPerTurn + float64(n-1)*float64(g)
		total += prefixFromPrev*p.CachedRead/1e6 + float64(g)*p.CacheWrite/1e6
		total += intraTurnToolCost(prefixFromPrev+float64(g), extraCalls, s, p)
	}
	return total
}

// CumulativeCostB models opening a new session and injecting compressed history
// as a single setup request.
//
// Setup: cache write (base) — one-time cost of injecting cc-session output.
//
// Turn 1:
//
//	Call 1: cache read (base from setup) + cache write (P)
//	Calls 2..K: cache read (growing) + cache write (tool I/O)
//
// Turn N (N>=2): same structure as A but with smaller base, cross-turn write = growth.
func CumulativeCostB(turns int, x int, filteredTokens int, sp CostParams, p Pricing) float64 {
	return CumulativeCostBWithInjectPages(turns, x, filteredTokens, 1, sp, p)
}

// CumulativeCostBWithInjectPages models opening a new session and injecting
// compressed history with cc-session inject pagination.
//
// For one page, setup keeps the historical one-shot model. For multiple pages,
// setup is modeled as repeated API requests:
//
//	initial overhead cache write
//	each page: read(overhead + previous page tokens) + write(page tokens)
//
// The active conversation after setup is still overhead + filteredTokens.
func CumulativeCostBWithInjectPages(turns int, x int, filteredTokens int, injectPages int, sp CostParams, p Pricing) float64 {
	base := sp.Overhead + filteredTokens
	total := newSessionSetupCost(filteredTokens, injectPages, sp, p)
	extraCalls := extraCallsPerTurn(sp.K)
	s := sp.ToolIOPerCall
	g := sp.Growth
	toolIOPerTurn := float64(s) * extraCalls
	for n := 1; n <= turns; n++ {
		var prefixFromPrev float64
		var crossTurnWrite int
		if n == 1 {
			prefixFromPrev = float64(base)
			crossTurnWrite = sp.Prompt // setup response negligible
		} else {
			prefixFromPrev = float64(base+sp.Prompt) + float64(n-1)*toolIOPerTurn + float64(n-2)*float64(g)
			crossTurnWrite = g // R + P, same as Scenario A
		}
		total += prefixFromPrev*p.CachedRead/1e6 + float64(crossTurnWrite)*p.CacheWrite/1e6
		total += intraTurnToolCost(prefixFromPrev+float64(crossTurnWrite), extraCalls, s, p)
	}
	return total
}

func newSessionSetupCost(filteredTokens int, injectPages int, sp CostParams, p Pricing) float64 {
	base := sp.Overhead + filteredTokens
	if injectPages <= 1 {
		return float64(base) * p.CacheWrite / 1e6
	}

	pageTokens := float64(filteredTokens) / float64(injectPages)
	readTokens := 0.0
	for page := 0; page < injectPages; page++ {
		readTokens += float64(sp.Overhead) + float64(page)*pageTokens
	}
	writeTokens := float64(sp.Overhead) + float64(filteredTokens)

	return readTokens*p.CachedRead/1e6 + writeTokens*p.CacheWrite/1e6
}

func extraCallsPerTurn(k float64) float64 {
	if k <= 1 {
		return 0
	}
	return k - 1
}

func intraTurnToolCost(firstPrefix float64, extraCalls float64, toolIOPerCall int, p Pricing) float64 {
	if extraCalls <= 0 {
		return 0
	}

	fullCalls := int(extraCalls)
	fractionalCall := extraCalls - float64(fullCalls)
	toolIO := float64(toolIOPerCall)

	readTokens := float64(fullCalls)*firstPrefix + toolIO*float64(fullCalls*(fullCalls-1))/2
	if fractionalCall > 0 {
		readTokens += fractionalCall * (firstPrefix + toolIO*float64(fullCalls))
	}
	writeTokens := extraCalls * toolIO

	return readTokens*p.CachedRead/1e6 + writeTokens*p.CacheWrite/1e6
}

// ComputeCostMetrics populates the cost-related fields of r.
func ComputeCostMetrics(r *Result, overheadTokens int, p Pricing) {
	sp := NewCostParams(*r, overheadTokens)
	r.BreakEven = -1
	for n := 1; n <= 200; n++ {
		if CumulativeCostBWithInjectPages(n, r.ContextTokens, r.FilteredTokens, r.InjectPages, sp, p) < CumulativeCostA(n, r.ContextTokens, sp, p) {
			r.BreakEven = n
			break
		}
	}

	cost10A := CumulativeCostA(10, r.ContextTokens, sp, p)
	cost10B := CumulativeCostBWithInjectPages(10, r.ContextTokens, r.FilteredTokens, r.InjectPages, sp, p)
	if cost10A > 0 {
		r.Saving10Pct = (cost10A - cost10B) * 100.0 / cost10A
	}

	cost100A := CumulativeCostA(100, r.ContextTokens, sp, p)
	cost100B := CumulativeCostBWithInjectPages(100, r.ContextTokens, r.FilteredTokens, r.InjectPages, sp, p)
	if cost100A > 0 {
		r.Saving100Pct = (cost100A - cost100B) * 100.0 / cost100A
	}

	r.WarmBreakEven = -1
	for n := 1; n <= 200; n++ {
		if CumulativeCostBWithInjectPages(n, r.ContextTokens, r.FilteredTokens, r.InjectPages, sp, p) < CumulativeCostAWarm(n, r.ContextTokens, sp, p) {
			r.WarmBreakEven = n
			break
		}
	}

	cost10Warm := CumulativeCostAWarm(10, r.ContextTokens, sp, p)
	if cost10Warm > 0 {
		r.WarmSaving10Pct = (cost10Warm - cost10B) * 100.0 / cost10Warm
	}

	cost100Warm := CumulativeCostAWarm(100, r.ContextTokens, sp, p)
	if cost100Warm > 0 {
		r.WarmSaving100Pct = (cost100Warm - cost100B) * 100.0 / cost100Warm
	}
}
