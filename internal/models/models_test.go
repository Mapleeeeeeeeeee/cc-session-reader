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
		{"opus-4-8", Opus48},
		{"opus-4-7", Opus47},
		{"opus-4-6", Opus46},
		{"sonnet", Sonnet55},
		{"sonnet-5-5", Sonnet55},
		{"sonnet-4-6", Sonnet46},
		{"fable", Fable51},
		{"fable-5-1", Fable51},
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
		{Sonnet55, 0.10}, {Sonnet46, 0.10}, {Opus48, 0.10},
		{Opus55, 0.05}, {Fable51, 0.025},
	}
	for _, tt := range tests {
		if want := tt.m.Pricing.BaseInput * tt.ratio; math.Abs(tt.m.Pricing.CachedRead-want) > 1e-9 {
			t.Errorf("%s CachedRead = %v, want %v (%v of BaseInput %v)",
				tt.m.Name, tt.m.Pricing.CachedRead, want, tt.ratio, tt.m.Pricing.BaseInput)
		}
		if want := tt.m.Pricing.BaseInput * 1.25; math.Abs(tt.m.Pricing.CacheWrite-want) > 1e-9 {
			t.Errorf("%s CacheWrite = %v, want %v (1.25x BaseInput)", tt.m.Name, tt.m.Pricing.CacheWrite, want)
		}
	}
}
