package benchmark

import (
	"strings"
	"testing"
)

func Test_ResolveModel_GivenKnownAlias_ThenReturnsExpectedConfig(t *testing.T) {
	tests := []struct {
		name           string
		alias          string
		wantPricing    Pricing
		wantTokenCount string
	}{
		{
			name:           "sonnet",
			alias:          "sonnet",
			wantPricing:    PricingSonnet,
			wantTokenCount: TokenCountModelSonnet,
		},
		{
			name:           "opus",
			alias:          "opus",
			wantPricing:    PricingOpus,
			wantTokenCount: TokenCountModelOpus48,
		},
		{
			name:           "opus-4-8",
			alias:          "opus-4-8",
			wantPricing:    PricingOpus,
			wantTokenCount: TokenCountModelOpus48,
		},
		{
			name:           "opus-4-7",
			alias:          "opus-4-7",
			wantPricing:    PricingOpus,
			wantTokenCount: TokenCountModelOpus47,
		},
		{
			name:           "opus-4-6",
			alias:          "opus-4-6",
			wantPricing:    PricingOpus,
			wantTokenCount: TokenCountModelOpus46,
		},
		{
			name:           "fable",
			alias:          "fable",
			wantPricing:    PricingFable,
			wantTokenCount: TokenCountModelFable,
		},
		{
			name:           "fable-5-1",
			alias:          "fable-5-1",
			wantPricing:    PricingFable,
			wantTokenCount: TokenCountModelFable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveModel(tt.alias)
			if err != nil {
				t.Fatalf("ResolveModel(%q) returned error: %v", tt.alias, err)
			}
			if got.Pricing != tt.wantPricing {
				t.Errorf("Pricing = %+v, want %+v", got.Pricing, tt.wantPricing)
			}
			if got.TokenCountModel != tt.wantTokenCount {
				t.Errorf("TokenCountModel = %q, want %q", got.TokenCountModel, tt.wantTokenCount)
			}
		})
	}
}

// Regression guard: PricingFable's cache read must stay at 2.5% of base input
// ($0.25/MTok), not the 10% ratio every other tier in this file uses.
func Test_PricingFable_ThenCacheReadIsTwoPointFivePercentOfBaseInput(t *testing.T) {
	want := PricingFable.BaseInput * 0.025
	if PricingFable.CachedRead != want {
		t.Errorf("PricingFable.CachedRead = %v, want %v (2.5%% of BaseInput %v)",
			PricingFable.CachedRead, want, PricingFable.BaseInput)
	}
}

func Test_ResolveModel_GivenUnknownAlias_ThenErrorListsFableAliases(t *testing.T) {
	_, err := ResolveModel("nonsense")
	if err == nil {
		t.Fatal("ResolveModel(\"nonsense\") returned nil error, want unknown model error")
	}
	if !strings.Contains(err.Error(), "fable") {
		t.Errorf("error = %v, want it to list the fable aliases", err)
	}
}
