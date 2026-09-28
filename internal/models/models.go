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

// Model describes one Claude model.
type Model struct {
	Name    string // canonical alias, e.g. "opus-5-5"
	Family  string // family alias when this is the newest in its family, e.g. "opus"; "" otherwise
	ID      string // Anthropic API model ID
	Pricing Pricing
}

// Alias returns the family alias if set, otherwise the canonical name.
func (m Model) Alias() string {
	if m.Family != "" {
		return m.Family
	}
	return m.Name
}

// Opus 4.x share one price list (4.6, 4.7, 4.8).
var pricingOpus4x = Pricing{CachedRead: 0.50, CacheWrite: 6.25, BaseInput: 5.00}

var (
	Sonnet55 = Model{Name: "sonnet-5-5", Family: "sonnet", ID: "claude-sonnet-5-5",
		Pricing: Pricing{CachedRead: 0.20, CacheWrite: 2.50, BaseInput: 2.00}}
	Sonnet46 = Model{Name: "sonnet-4-6", ID: "claude-sonnet-4-6",
		Pricing: Pricing{CachedRead: 0.30, CacheWrite: 3.75, BaseInput: 3.00}}

	// Opus 5.5's cache read is 5% of base input rather than the 10% Opus 4.x and
	// Sonnet use (Anthropic pricing docs: "a cache hit costs 5% of the standard
	// input price"), so its cost-savings numbers are not directly comparable to
	// those rows.
	Opus55 = Model{Name: "opus-5-5", Family: "opus", ID: "claude-opus-5-5",
		Pricing: Pricing{CachedRead: 0.20, CacheWrite: 5.00, BaseInput: 4.00}}
	Opus48 = Model{Name: "opus-4-8", ID: "claude-opus-4-8", Pricing: pricingOpus4x}
	Opus47 = Model{Name: "opus-4-7", ID: "claude-opus-4-7", Pricing: pricingOpus4x}
	Opus46 = Model{Name: "opus-4-6", ID: "claude-opus-4-6", Pricing: pricingOpus4x}

	// Fable 5.1's cache read is 2.5% of base input (Anthropic pricing docs: "a
	// cache hit costs 2.5% of the standard input price"), lower than the ratios
	// above, so its cost-savings numbers are not directly comparable either.
	Fable51 = Model{Name: "fable-5-1", Family: "fable", ID: "claude-fable-5-1",
		Pricing: Pricing{CachedRead: 0.25, CacheWrite: 12.50, BaseInput: 10.00}}
)

// DefaultBenchmark is the model `cc-session benchmark` uses without --model.
var DefaultBenchmark = Opus55

// DefaultCountTokens is the model used for count_tokens when none is given.
var DefaultCountTokens = Sonnet55

// registry lists every known model, in the order aliases are shown to users.
var registry = []Model{Opus55, Opus48, Opus47, Opus46, Sonnet55, Sonnet46, Fable51}

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
