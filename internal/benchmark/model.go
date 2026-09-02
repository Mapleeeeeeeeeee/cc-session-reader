package benchmark

import "fmt"

// Pricing holds per-million-token rates for a model tier.
type Pricing struct {
	CachedRead float64 // $/M tokens
	CacheWrite float64 // $/M tokens
	BaseInput  float64 // $/M tokens (uncached, after last breakpoint)
}

var PricingOpus = Pricing{CachedRead: 0.50, CacheWrite: 6.25, BaseInput: 5.00}
var PricingSonnet = Pricing{CachedRead: 0.30, CacheWrite: 3.75, BaseInput: 3.00}

// PricingFable is Claude Fable 5.1's pricing. Unlike every other tier here, its
// cache read is 2.5% of base input rather than the usual 10% (source: Anthropic
// pricing docs, "a cache hit costs 2.5% of the standard input price"), so its
// cost-savings numbers are not directly comparable to the Opus/Sonnet rows.
var PricingFable = Pricing{CachedRead: 0.25, CacheWrite: 12.50, BaseInput: 10.00}

const (
	TokenCountModelOpus46 = "claude-opus-4-6"
	TokenCountModelOpus47 = "claude-opus-4-7"
	TokenCountModelOpus48 = "claude-opus-4-8"
	TokenCountModelSonnet = "claude-sonnet-4-6"
	TokenCountModelFable  = "claude-fable-5-1"
)

// ModelConfig bundles the pricing and token-counting model for a given model alias.
type ModelConfig struct {
	Pricing         Pricing
	TokenCountModel string
}

// ResolveModel maps a user-facing model alias to its ModelConfig.
func ResolveModel(model string) (ModelConfig, error) {
	switch model {
	case "sonnet":
		return ModelConfig{Pricing: PricingSonnet, TokenCountModel: TokenCountModelSonnet}, nil
	case "opus", "opus-4-8":
		return ModelConfig{Pricing: PricingOpus, TokenCountModel: TokenCountModelOpus48}, nil
	case "opus-4-7":
		return ModelConfig{Pricing: PricingOpus, TokenCountModel: TokenCountModelOpus47}, nil
	case "opus-4-6":
		return ModelConfig{Pricing: PricingOpus, TokenCountModel: TokenCountModelOpus46}, nil
	case "fable", "fable-5-1":
		return ModelConfig{Pricing: PricingFable, TokenCountModel: TokenCountModelFable}, nil
	default:
		return ModelConfig{}, fmt.Errorf("unknown model %q: must be opus, opus-4-6, opus-4-7, opus-4-8, sonnet, fable, or fable-5-1", model)
	}
}

// RatioPct returns the ratio of NewContextTokens to ContextTokens as a percentage.
func (r Result) RatioPct() float64 {
	if r.ContextTokens == 0 {
		return 0
	}
	return float64(r.NewContextTokens) / float64(r.ContextTokens) * 100
}

// Result holds per-session benchmark output.
type Result struct {
	ShortID          string
	ContextTokens    int
	FilteredTokens   int
	NewContextTokens int
	SavedPct         float64
	CallsPerTurn     float64
	ToolIOPerCall    int // derived from actual PerTool data
	AvgResponse      int // derived from TotalOutputTokens / APICallCount
	Prompt           int // derived from context growth, or fallback perTurnPrompt
	InjectPages      int // pages needed by cc-session inject; <=1 keeps one-shot setup
	BreakEven        int
	Saving10Pct      float64
	Saving100Pct     float64
	WarmBreakEven    int
	WarmSaving10Pct  float64
	WarmSaving100Pct float64
}
