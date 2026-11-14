package benchmark

import "github.com/Mapleeeeeeeeeee/cc-session-reader/internal/models"

// Cost model.
//
// Pricing: https://platform.claude.com/docs/en/build-with-claude/prompt-caching
//
//	cache_read     = 0.1x base  (prefix matching previous cache; 0.05x or 0.025x on some models)
//	cache_write_5m = 1.25x base (new content written to the 5-minute cache)
//	cache_write_1h = 2x base    (new content written to the 1-hour cache)
//	input          = 1.0x base  (after last cache breakpoint — near zero with auto-caching)
//
// Claude Code writes the main conversation's cache with the 1-hour TTL and
// subagent and SDK-started sessions with the 5-minute TTL, so the write rate is
// not a constant. CostParams.OneHourWriteShare is the session's measured share
// of 1-hour write tokens; every cache write in every scenario is billed at
// models.Pricing.CacheWriteAt(share). Both scenarios use the same share.
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
//	Turn N:   thinking generated         → output_tokens               @ output rate
//	Turn N+1: first time as input        → cache_creation_input_tokens @ cache write rate
//	Turn N+2: subsequent turns as input  → cache_read_input_tokens     @ cache read rate
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

	// OneHourWriteShare is the share (0 to 1) of every cache write billed at the
	// 1-hour TTL rate; the rest is billed at the 5-minute rate.
	OneHourWriteShare float64
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

		OneHourWriteShare: r.OneHourWriteShare,
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
func CumulativeCostA(turns int, x int, sp CostParams, m models.Model) float64 {
	total := 0.0
	extraCalls := extraCallsPerTurn(sp.K)
	s := sp.ToolIOPerCall
	g := sp.Growth
	toolIOPerTurn := float64(s) * extraCalls
	for n := 1; n <= turns; n++ {
		if n == 1 {
			total += requestCost(0, float64(x+sp.Prompt), sp.OneHourWriteShare, m)
			total += intraTurnToolCost(float64(x+sp.Prompt), extraCalls, s, sp.OneHourWriteShare, m)
		} else {
			prefixFromPrev := float64(x+sp.Prompt) + float64(n-1)*toolIOPerTurn + float64(n-2)*float64(g)
			total += requestCost(prefixFromPrev, float64(g), sp.OneHourWriteShare, m)
			total += intraTurnToolCost(prefixFromPrev+float64(g), extraCalls, s, sp.OneHourWriteShare, m)
		}
	}
	return total
}

// CumulativeCostAWarm models staying in an existing session when cache is still warm.
//
// Cache TTL source: https://platform.claude.com/docs/en/build-with-claude/prompt-caching
// While you keep working within the cache TTL, the entire prefix X is already cached,
// so Turn 1 behaves like Turn N≥2 in the cold model (cache read, not cache write).
//
// Turn N (all N, including N=1):
//
//	Call 1: cache read (prefix from previous turn, or X when N=1) + cache write (R + P)
//	Calls 2..K: cache read (growing prefix) + cache write (tool I/O)
func CumulativeCostAWarm(turns int, x int, sp CostParams, m models.Model) float64 {
	total := 0.0
	extraCalls := extraCallsPerTurn(sp.K)
	s := sp.ToolIOPerCall
	g := sp.Growth
	toolIOPerTurn := float64(s) * extraCalls
	for n := 1; n <= turns; n++ {
		// n=1: prefixFromPrev = X (fully cached); growth shifts by 1 vs cold because
		// the previous turn's R is already in cache before our counting starts.
		prefixFromPrev := float64(x) + float64(n-1)*toolIOPerTurn + float64(n-1)*float64(g)
		total += requestCost(prefixFromPrev, float64(g), sp.OneHourWriteShare, m)
		total += intraTurnToolCost(prefixFromPrev+float64(g), extraCalls, s, sp.OneHourWriteShare, m)
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
func CumulativeCostB(turns int, x int, filteredTokens int, sp CostParams, m models.Model) float64 {
	return CumulativeCostBWithInjectPages(turns, filteredTokens, 1, sp, m)
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
func CumulativeCostBWithInjectPages(turns int, filteredTokens int, injectPages int, sp CostParams, m models.Model) float64 {
	base := sp.Overhead + filteredTokens
	total := newSessionSetupCost(filteredTokens, injectPages, sp, m)
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
		total += requestCost(prefixFromPrev, float64(crossTurnWrite), sp.OneHourWriteShare, m)
		total += intraTurnToolCost(prefixFromPrev+float64(crossTurnWrite), extraCalls, s, sp.OneHourWriteShare, m)
	}
	return total
}

func newSessionSetupCost(filteredTokens int, injectPages int, sp CostParams, m models.Model) float64 {
	base := sp.Overhead + filteredTokens
	if injectPages <= 1 {
		return requestCost(0, float64(base), sp.OneHourWriteShare, m)
	}

	pageTokens := float64(filteredTokens) / float64(injectPages)
	if p, isOneTier := singleTierPricing(m, float64(sp.Overhead), float64(base)); isOneTier {
		readTokens := 0.0
		for page := 0; page < injectPages; page++ {
			readTokens += float64(sp.Overhead) + float64(page)*pageTokens
		}
		writeTokens := float64(sp.Overhead) + float64(filteredTokens)

		return readTokens*p.CachedRead/1e6 + writeTokens*p.CacheWriteAt(sp.OneHourWriteShare)/1e6
	}

	// The setup prompt grows across the model's price threshold, so each request is priced on its own.
	total := requestCost(0, float64(sp.Overhead), sp.OneHourWriteShare, m)
	for page := 0; page < injectPages; page++ {
		total += requestCost(float64(sp.Overhead)+float64(page)*pageTokens, pageTokens, sp.OneHourWriteShare, m)
	}
	return total
}

func extraCallsPerTurn(k float64) float64 {
	if k <= 1 {
		return 0
	}
	return k - 1
}

// intraTurnToolCost prices the extra tool round-trip calls of one turn. Call j
// (1-based) reads firstPrefix + toolIO*(j-1) from cache and writes toolIO, so
// its prompt is firstPrefix + toolIO*j. The last call is weighted by the
// fractional part of extraCalls.
func intraTurnToolCost(firstPrefix float64, extraCalls float64, toolIOPerCall int, oneHourShare float64, m models.Model) float64 {
	if extraCalls <= 0 {
		return 0
	}

	fullCalls := int(extraCalls)
	fractionalCall := extraCalls - float64(fullCalls)
	toolIO := float64(toolIOPerCall)

	lastCall := fullCalls
	if fractionalCall > 0 {
		lastCall++
	}
	if p, isOneTier := singleTierPricing(m, firstPrefix+toolIO, firstPrefix+toolIO*float64(lastCall)); isOneTier {
		readTokens := float64(fullCalls)*firstPrefix + toolIO*float64(fullCalls*(fullCalls-1))/2
		if fractionalCall > 0 {
			readTokens += fractionalCall * (firstPrefix + toolIO*float64(fullCalls))
		}
		writeTokens := extraCalls * toolIO

		return readTokens*p.CachedRead/1e6 + writeTokens*p.CacheWriteAt(oneHourShare)/1e6
	}

	// The prompt grows across the model's price threshold during this turn, so each call is priced on its own.
	total := 0.0
	for call := 1; call <= lastCall; call++ {
		weight := 1.0
		if call > fullCalls {
			weight = fractionalCall
		}
		total += weight * requestCost(firstPrefix+toolIO*float64(call-1), toolIO, oneHourShare, m)
	}
	return total
}

// requestCost prices one API request that reads readTokens from cache and
// writes writeTokens to it. The request's rates follow its prompt size, which
// is readTokens + writeTokens; oneHourShare of the write uses the 1-hour TTL.
func requestCost(readTokens float64, writeTokens float64, oneHourShare float64, m models.Model) float64 {
	p := m.PricingFor(readTokens + writeTokens)
	return readTokens*p.CachedRead/1e6 + writeTokens*p.CacheWriteAt(oneHourShare)/1e6
}

// singleTierPricing reports whether requests with prompts from smallestPrompt
// to largestPrompt are all billed at the same rates, and returns those rates.
// A non-tiered model always reports true. Callers keep their closed-form
// formulas for that case so results for non-tiered models stay bit-for-bit
// the same as before tiered pricing existed.
func singleTierPricing(m models.Model, smallestPrompt float64, largestPrompt float64) (models.Pricing, bool) {
	p := m.PricingFor(smallestPrompt)
	return p, p == m.PricingFor(largestPrompt)
}

// ComputeCostMetrics populates the cost-related fields of r.
func ComputeCostMetrics(r *Result, overheadTokens int, m models.Model) {
	sp := NewCostParams(*r, overheadTokens)
	r.BreakEven = -1
	for n := 1; n <= 200; n++ {
		if CumulativeCostBWithInjectPages(n, r.FilteredTokens, r.InjectPages, sp, m) < CumulativeCostA(n, r.ContextTokens, sp, m) {
			r.BreakEven = n
			break
		}
	}

	cost10A := CumulativeCostA(10, r.ContextTokens, sp, m)
	cost10B := CumulativeCostBWithInjectPages(10, r.FilteredTokens, r.InjectPages, sp, m)
	if cost10A > 0 {
		r.Saving10Pct = (cost10A - cost10B) * 100.0 / cost10A
	}

	cost100A := CumulativeCostA(100, r.ContextTokens, sp, m)
	cost100B := CumulativeCostBWithInjectPages(100, r.FilteredTokens, r.InjectPages, sp, m)
	if cost100A > 0 {
		r.Saving100Pct = (cost100A - cost100B) * 100.0 / cost100A
	}

	r.WarmBreakEven = -1
	for n := 1; n <= 200; n++ {
		if CumulativeCostBWithInjectPages(n, r.FilteredTokens, r.InjectPages, sp, m) < CumulativeCostAWarm(n, r.ContextTokens, sp, m) {
			r.WarmBreakEven = n
			break
		}
	}

	cost10Warm := CumulativeCostAWarm(10, r.ContextTokens, sp, m)
	if cost10Warm > 0 {
		r.WarmSaving10Pct = (cost10Warm - cost10B) * 100.0 / cost10Warm
	}

	cost100Warm := CumulativeCostAWarm(100, r.ContextTokens, sp, m)
	if cost100Warm > 0 {
		r.WarmSaving100Pct = (cost100Warm - cost100B) * 100.0 / cost100Warm
	}
}
