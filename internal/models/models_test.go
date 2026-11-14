package models

import (
	"math"
	"strings"
	"testing"
)

func Test_Resolve_GivenKnownAlias_ThenReturnsExpectedModel(t *testing.T) {
	tests := []struct {
		alias string
		want  Model
	}{
		{"opus", Opus55},
		{"opus-5-5", Opus55},
		{"opus-5", Opus5},
		{"opus-4-8", Opus48},
		{"opus-4-7", Opus47},
		{"opus-4-6", Opus46},
		{"sonnet", Sonnet55},
		{"sonnet-5-5", Sonnet55},
		{"sonnet-5", Sonnet5},
		{"sonnet-4-6", Sonnet46},
		{"haiku", Haiku55},
		{"haiku-5-5", Haiku55},
		{"fable", Fable52},
		{"fable-5-2", Fable52},
		{"fable-5-1", Fable51},
		{"fable-5", Fable5},
	}
	for _, tt := range tests {
		t.Run(tt.alias, func(t *testing.T) {
			got, err := Resolve(tt.alias)
			if err != nil {
				t.Fatalf("Resolve(%q) returned error: %v", tt.alias, err)
			}
			if got != tt.want {
				t.Errorf("Resolve(%q) = %+v, want %+v", tt.alias, got, tt.want)
			}
		})
	}
}

func Test_Resolve_GivenUnknownAlias_ThenErrorListsEveryAlias(t *testing.T) {
	_, err := Resolve("nonsense")
	if err == nil {
		t.Fatal("Resolve(\"nonsense\") returned nil error")
	}
	for _, alias := range Aliases() {
		if !strings.Contains(err.Error(), alias) {
			t.Errorf("error %q missing alias %q", err, alias)
		}
	}
}

func Test_Registry_ThenNamesIDsAndFamiliesAreUnique(t *testing.T) {
	families := map[string]string{}
	names := map[string]bool{}
	ids := map[string]bool{}
	for _, m := range registry {
		if names[m.Name] || ids[m.ID] {
			t.Errorf("duplicate name or ID for %+v", m)
		}
		names[m.Name], ids[m.ID] = true, true
		if m.Family != "" {
			if prev, dup := families[m.Family]; dup {
				t.Errorf("family %q claimed by both %s and %s", m.Family, prev, m.Name)
			}
			families[m.Family] = m.Name
		}
	}
}

func Test_Defaults_ThenAreRegisteredModels(t *testing.T) {
	for name, d := range map[string]Model{"DefaultBenchmark": DefaultBenchmark, "DefaultCountTokens": DefaultCountTokens} {
		if got, err := Resolve(d.Name); err != nil || got != d {
			t.Errorf("%s = %+v is not in the registry", name, d)
		}
	}
}

// Cache-read ratios differ per model and are not comparable across rows; pin
// them so a price edit that breaks the ratio is caught.
func Test_Pricing_ThenCacheReadRatiosMatchPricingDocs(t *testing.T) {
	tests := []struct {
		m     Model
		ratio float64
	}{
		{Sonnet5, 0.10}, {Sonnet46, 0.10}, {Opus5, 0.10}, {Opus48, 0.10}, {Fable5, 0.10}, {Haiku55, 0.10},
		{Sonnet55, 0.05}, {Opus55, 0.05},
		{Fable52, 0.025}, {Fable51, 0.025},
	}
	for _, tt := range tests {
		if want := tt.m.Pricing.BaseInput * tt.ratio; math.Abs(tt.m.Pricing.CachedRead-want) > 1e-9 {
			t.Errorf("%s CachedRead = %v, want %v (%v of BaseInput %v)",
				tt.m.Name, tt.m.Pricing.CachedRead, want, tt.ratio, tt.m.Pricing.BaseInput)
		}
		if want := tt.m.Pricing.BaseInput * 1.25; math.Abs(tt.m.Pricing.CacheWrite5m-want) > 1e-9 {
			t.Errorf("%s CacheWrite5m = %v, want %v (1.25x BaseInput)", tt.m.Name, tt.m.Pricing.CacheWrite5m, want)
		}
		if want := tt.m.Pricing.BaseInput * 2; math.Abs(tt.m.Pricing.CacheWrite1h-want) > 1e-9 {
			t.Errorf("%s CacheWrite1h = %v, want %v (2x BaseInput)", tt.m.Name, tt.m.Pricing.CacheWrite1h, want)
		}
	}
}

// Haiku 5.5's higher price level follows the same write multiples as its
// standard one: 0.625 is 1.25x and 1.00 is 2x of its $0.50 base input.
func Test_Pricing_GivenHaiku55LongPrompt_ThenCacheWritesAreSameMultiplesOfBaseInput(t *testing.T) {
	p := Haiku55.LongPromptPricing
	if math.Abs(p.CacheWrite5m-p.BaseInput*1.25) > 1e-9 || math.Abs(p.CacheWrite1h-p.BaseInput*2) > 1e-9 {
		t.Errorf("Haiku55 long-prompt writes = %v / %v, want 1.25x / 2x of %v", p.CacheWrite5m, p.CacheWrite1h, p.BaseInput)
	}
}

func Test_CacheWriteAt(t *testing.T) {
	p := Pricing{CacheWrite5m: 6.25, CacheWrite1h: 10.00}
	tests := []struct {
		name         string
		oneHourShare float64
		want         float64
	}{
		{"GivenAllFiveMinute_ThenFiveMinuteRate", 0, 6.25},
		{"GivenAllOneHour_ThenOneHourRate", 1, 10.00},
		// 0.25*6.25 + 0.75*10.00 = 1.5625 + 7.5 = 9.0625
		{"GivenThreeQuartersOneHour_ThenWeightedRate", 0.75, 9.0625},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.CacheWriteAt(tt.oneHourShare); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("CacheWriteAt(%v) = %v, want %v", tt.oneHourShare, got, tt.want)
			}
		})
	}
}

// Rates are the official Haiku 5.5 price list, $/MTok.
func Test_PricingFor_GivenHaiku55_ThenRatesFollowPromptSize(t *testing.T) {
	standard := Pricing{CachedRead: 0.01, CacheWrite5m: 0.125, CacheWrite1h: 0.20, BaseInput: 0.10}
	higher := Pricing{CachedRead: 0.05, CacheWrite5m: 0.625, CacheWrite1h: 1.00, BaseInput: 0.50}
	tests := []struct {
		name         string
		promptTokens float64
		want         Pricing
	}{
		{"EmptyPrompt", 0, standard},
		{"BelowThreshold", 99_999, standard},
		{"ExactlyAtThreshold", 100_000, standard},
		{"OneTokenOverThreshold", 100_001, higher},
		{"FarOverThreshold", 900_000, higher},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Haiku55.PricingFor(tt.promptTokens); got != tt.want {
				t.Errorf("Haiku55.PricingFor(%v) = %+v, want %+v", tt.promptTokens, got, tt.want)
			}
		})
	}
}

func Test_PricingFor_GivenModelWithSinglePriceList_ThenPromptSizeDoesNotChangeRates(t *testing.T) {
	for _, promptTokens := range []float64{0, 100_000, 100_001, 900_000} {
		if got := Opus48.PricingFor(promptTokens); got != Opus48.Pricing {
			t.Errorf("Opus48.PricingFor(%v) = %+v, want %+v", promptTokens, got, Opus48.Pricing)
		}
	}
}
