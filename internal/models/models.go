// Package models is the single source of truth for the Claude models this
// tool knows about: their API IDs, per-token pricing, user-facing aliases and
// the default model used for token counting.
//
// Help text and error messages derive from registry; docs/benchmark.md is the
// only place that must be updated by hand.
package models

import (
	"fmt"
	"strings"
)

// Pricing holds per-million-token rates for a model tier.
type Pricing struct {
	CachedRead float64 // $/M tokens
	CacheWrite float64 // $/M tokens
	BaseInput  float64 // $/M tokens (uncached, after last breakpoint)
}

// LongPromptThreshold is the prompt size, in tokens, above which a request to a
// model with LongPromptPricing is billed at the higher rates.
const LongPromptThreshold = 100_000

// Model describes one Claude model.
type Model struct {
	Name    string // canonical alias, e.g. "opus-5-5"
	Family  string // family alias when this is the newest in its family, e.g. "opus"; "" otherwise
	ID      string // Anthropic API model ID
	Pricing Pricing
	// LongPromptPricing replaces Pricing for a request whose prompt is over
	// LongPromptThreshold tokens; nil for models with a single price list.
	LongPromptPricing *Pricing
}

// PricingFor returns the rates for one API request whose prompt is promptTokens
// long. The prompt counts every input token of the request: cache reads, cache
// writes and uncached input. A prompt of exactly LongPromptThreshold tokens is
// still billed at the standard rates.
func (m Model) PricingFor(promptTokens float64) Pricing {
	if m.LongPromptPricing != nil && promptTokens > LongPromptThreshold {
		return *m.LongPromptPricing
	}
	return m.Pricing
}

// Alias returns the family alias if set, otherwise the canonical name.
func (m Model) Alias() string {
	if m.Family != "" {
		return m.Family
	}
	return m.Name
}

// Cache read is billed at a different share of base input depending on the
// model (Anthropic pricing docs): 0.025x for Fable 5.2 and 5.1, 0.05x for
// Opus 5.5 and Sonnet 5.5, and 0.1x for every other model, Haiku 5.5 included
// at both of its price levels. Cost-savings numbers
// are therefore only directly comparable between models in the same group.

// Opus 4.6, 4.7, 4.8 and Opus 5 share one price list.
var pricingOpusStandard = Pricing{CachedRead: 0.50, CacheWrite: 6.25, BaseInput: 5.00}

var (
	Sonnet55 = Model{Name: "sonnet-5-5", Family: "sonnet", ID: "claude-sonnet-5-5",
		Pricing: Pricing{CachedRead: 0.10, CacheWrite: 2.50, BaseInput: 2.00}}
	Sonnet5 = Model{Name: "sonnet-5", ID: "claude-sonnet-5",
		Pricing: Pricing{CachedRead: 0.20, CacheWrite: 2.50, BaseInput: 2.00}}
	Sonnet46 = Model{Name: "sonnet-4-6", ID: "claude-sonnet-4-6",
		Pricing: Pricing{CachedRead: 0.30, CacheWrite: 3.75, BaseInput: 3.00}}

	Haiku55 = Model{Name: "haiku-5-5", Family: "haiku", ID: "claude-haiku-5-5",
		Pricing:           Pricing{CachedRead: 0.01, CacheWrite: 0.125, BaseInput: 0.10},
		LongPromptPricing: &Pricing{CachedRead: 0.05, CacheWrite: 0.625, BaseInput: 0.50}}

	Opus55 = Model{Name: "opus-5-5", Family: "opus", ID: "claude-opus-5-5",
		Pricing: Pricing{CachedRead: 0.20, CacheWrite: 5.00, BaseInput: 4.00}}
	Opus5  = Model{Name: "opus-5", ID: "claude-opus-5", Pricing: pricingOpusStandard}
	Opus48 = Model{Name: "opus-4-8", ID: "claude-opus-4-8", Pricing: pricingOpusStandard}
	Opus47 = Model{Name: "opus-4-7", ID: "claude-opus-4-7", Pricing: pricingOpusStandard}
	Opus46 = Model{Name: "opus-4-6", ID: "claude-opus-4-6", Pricing: pricingOpusStandard}

	Fable52 = Model{Name: "fable-5-2", Family: "fable", ID: "claude-fable-5-2",
		Pricing: Pricing{CachedRead: 0.20, CacheWrite: 10.00, BaseInput: 8.00}}
	Fable51 = Model{Name: "fable-5-1", ID: "claude-fable-5-1",
		Pricing: Pricing{CachedRead: 0.25, CacheWrite: 12.50, BaseInput: 10.00}}
	Fable5 = Model{Name: "fable-5", ID: "claude-fable-5",
		Pricing: Pricing{CachedRead: 1.00, CacheWrite: 12.50, BaseInput: 10.00}}
)

// DefaultBenchmark is the model `cc-session benchmark` uses without --model.
var DefaultBenchmark = Opus55

// DefaultCountTokens is the model used for count_tokens when none is given.
var DefaultCountTokens = Sonnet55

// registry lists every known model, in the order aliases are shown to users.
var registry = []Model{
	Opus55, Opus5, Opus48, Opus47, Opus46,
	Sonnet55, Sonnet5, Sonnet46,
	Haiku55,
	Fable52, Fable51, Fable5,
}

// Aliases returns every accepted alias: each family alias followed by its
// canonical name, in registry order.
func Aliases() []string {
	var out []string
	for _, m := range registry {
		if m.Family != "" {
			out = append(out, m.Family)
		}
		out = append(out, m.Name)
	}
	return out
}

// Resolve maps a user-facing alias (family alias or canonical name) to its Model.
func Resolve(alias string) (Model, error) {
	for _, m := range registry {
		if alias == m.Name || (m.Family != "" && alias == m.Family) {
			return m, nil
		}
	}
	return Model{}, fmt.Errorf("unknown model %q: must be one of %s", alias, strings.Join(Aliases(), ", "))
}

// Usage renders the accepted aliases for flag help, e.g.
// "opus (opus-5-5), opus-4-8, ..., sonnet (sonnet-5-5), ...".
func Usage() string {
	parts := make([]string, 0, len(registry))
	for _, m := range registry {
		if m.Family != "" {
			parts = append(parts, fmt.Sprintf("%s (%s)", m.Family, m.Name))
		} else {
			parts = append(parts, m.Name)
		}
	}
	return strings.Join(parts, ", ")
}
