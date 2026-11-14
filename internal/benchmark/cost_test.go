package benchmark

import (
	"math"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/models"
)

const epsilon = 1e-9

// Expected values below assume the Opus 4.x rates ($5 / $6.25 / $0.50).
var testPricing = models.Opus48

func approxEqual(got, want float64) bool {
	return math.Abs(got-want) < epsilon
}

// Test params used across multiple test cases.
// x=100000, P=10000, AvgResponse=2000, g=12000, s=3000, overhead=40000, filteredTokens=50000
var testParams = CostParams{
	K:             1.0,
	ToolIOPerCall: 3000,
	AvgResponse:   2000,
	Prompt:        10000,
	Growth:        12000, // AvgResponse + Prompt
	Overhead:      40000,
}

var testParamsK2 = CostParams{
	K:             2.0,
	ToolIOPerCall: 3000,
	AvgResponse:   2000,
	Prompt:        10000,
	Growth:        12000,
	Overhead:      40000,
}

var testParamsK4478 = CostParams{
	K:             4.478,
	ToolIOPerCall: 3000,
	AvgResponse:   2000,
	Prompt:        10000,
	Growth:        12000,
	Overhead:      40000,
}

func Test_NewCostParams_GivenResult_ThenGrowthEqualsAvgResponsePlusPrompt(t *testing.T) {
	r := Result{
		CallsPerTurn:  1.6,
		ToolIOPerCall: 3000,
		AvgResponse:   2000,
		Prompt:        10000,
	}
	sp := NewCostParams(r, 40000)

	if sp.Growth != r.AvgResponse+r.Prompt {
		t.Errorf("Growth = %d, want %d (AvgResponse + Prompt)", sp.Growth, r.AvgResponse+r.Prompt)
	}
	if sp.K != r.CallsPerTurn {
		t.Errorf("K = %v, want %v", sp.K, r.CallsPerTurn)
	}
	if sp.ToolIOPerCall != r.ToolIOPerCall {
		t.Errorf("ToolIOPerCall = %d, want %d", sp.ToolIOPerCall, r.ToolIOPerCall)
	}
	if sp.AvgResponse != r.AvgResponse {
		t.Errorf("AvgResponse = %d, want %d", sp.AvgResponse, r.AvgResponse)
	}
	if sp.Prompt != r.Prompt {
		t.Errorf("Prompt = %d, want %d", sp.Prompt, r.Prompt)
	}
	if sp.Overhead != 40000 {
		t.Errorf("Overhead = %d, want 40000", sp.Overhead)
	}
}

func Test_CumulativeCosts_GivenFractionalK_ThenUseFractionalCallsPerTurn(t *testing.T) {
	// Regression guard: K=4.478 is displayed as 4.5 in the CLI, but math.Round(K)
	// silently modeled only 4 calls per turn.
	tests := []struct {
		name string
		got  float64
		want float64
	}{
		{
			name: "ColdExistingSession",
			got:  CumulativeCostA(1, 100000, testParamsK4478, testPricing),
			want: 0.9506535,
		},
		{
			name: "WarmExistingSession",
			got:  CumulativeCostAWarm(1, 100000, testParamsK4478, testPricing),
			want: 0.3916315,
		},
		{
			name: "NewSession",
			got:  CumulativeCostB(1, 100000, 50000, testParamsK4478, testPricing),
			want: 0.9157635,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !approxEqual(tt.got, tt.want) {
				t.Fatalf("cost with K=4.478 = %.10f, want %.10f", tt.got, tt.want)
			}
		})
	}
}

// CumulativeCostA — hand-derived expected values:
//
//	1 turn, K=1: call1 = (100000+10000) * 6.25/1e6 = 0.6875
//	1 turn, K=2: call1 = 0.6875; call2 = 110000*0.50/1e6 + 3000*6.25/1e6 = 0.07375 → 0.76125
//	2 turns, K=1: turn1 = 0.6875; turn2 = 110000*0.50/1e6 + 12000*6.25/1e6 = 0.130 → 0.8175
var costATests = []struct {
	name  string
	turns int
	x     int
	sp    CostParams
	want  float64
}{
	{
		name:  "GivenZeroTurns_ThenCostIsZero",
		turns: 0,
		x:     100000,
		sp:    testParams,
		want:  0.0,
	},
	{
		name:  "GivenSingleTurnK1_ThenOnlyCacheWrite",
		turns: 1,
		x:     100000,
		sp:    testParams,
		// call1: (x+P) * CacheWrite/1e6 = 110000 * 6.25/1e6 = 0.6875
		want: 0.6875,
	},
	{
		name:  "GivenSingleTurnK2_ThenCall2HasCacheReadPlusCacheWrite",
		turns: 1,
		x:     100000,
		sp:    testParamsK2,
		// call1: 110000 * 6.25/1e6 = 0.6875
		// call2: 110000 * 0.50/1e6 + 3000 * 6.25/1e6 = 0.055 + 0.01875 = 0.07375
		want: 0.76125,
	},
	{
		name:  "GivenTwoTurnsK1_ThenTurn2UsesCacheRead",
		turns: 2,
		x:     100000,
		sp:    testParams,
		// turn1: 0.6875
		// turn2: prefixFromPrev=110000; 110000*0.50/1e6 + 12000*6.25/1e6 = 0.055+0.075 = 0.130
		want: 0.8175,
	},
	{
		name:  "GivenTwoTurnsK2_ThenPrefixGrowsAcrossTurns",
		turns: 2,
		x:     100000,
		sp:    testParamsK2,
		// n=1 call1: 0.6875; call2: 0.07375
		// n=2: prefixFromPrev = x+P+(2-1)*3000+(2-2)*12000 = 113000
		//   call1: 113000*0.50/1e6 + 12000*6.25/1e6 = 0.0565+0.075 = 0.1315
		//   call2: prefix=113000+12000+3000*(2-2)=125000; 125000*0.50/1e6 + 3000*6.25/1e6 = 0.0625+0.01875 = 0.08125
		want: 0.974,
	},
}

func Test_CumulativeCostA(t *testing.T) {
	for _, tc := range costATests {
		t.Run(tc.name, func(t *testing.T) {
			got := CumulativeCostA(tc.turns, tc.x, tc.sp, testPricing)
			if !approxEqual(got, tc.want) {
				t.Errorf("CumulativeCostA(%d, %d) = %.10f, want %.10f", tc.turns, tc.x, got, tc.want)
			}
		})
	}
}

// CumulativeCostAWarm — hand-derived expected values:
//
//	1 turn, K=1: prefixFromPrev=x=100000; 100000*0.50/1e6 + 12000*6.25/1e6 = 0.05+0.075 = 0.125
//	2 turns, K=1: n1: 0.125; n2: prefixFromPrev=x+(2-1)*0+(2-1)*12000=112000;
//	              112000*0.50/1e6 + 12000*6.25/1e6 = 0.056+0.075 = 0.131 → total=0.256
var costAWarmTests = []struct {
	name  string
	turns int
	x     int
	sp    CostParams
	want  float64
}{
	{
		name:  "GivenZeroTurns_ThenCostIsZero",
		turns: 0,
		x:     100000,
		sp:    testParams,
		want:  0.0,
	},
	{
		name:  "GivenSingleTurnK1_ThenXIsReadFromCache",
		turns: 1,
		x:     100000,
		sp:    testParams,
		// prefixFromPrev = x = 100000; x*0.50/1e6 + g*6.25/1e6 = 0.05+0.075 = 0.125
		want: 0.125,
	},
	{
		name:  "GivenTwoTurnsK1_ThenPrefixIncludesGrowth",
		turns: 2,
		x:     100000,
		sp:    testParams,
		// n=1: 0.125
		// n=2: prefixFromPrev=100000+0+12000=112000; 112000*0.50/1e6 + 12000*6.25/1e6 = 0.056+0.075 = 0.131
		want: 0.256,
	},
	{
		name:  "GivenTwoTurnsK2_ThenIntraTurnCallsApplied",
		turns: 2,
		x:     100000,
		sp:    testParamsK2,
		// n=1: prefix=100000; call1: 100000*0.50/1e6+12000*6.25/1e6=0.05+0.075=0.125
		//        c=2: prefix=100000+12000+0=112000; 112000*0.50/1e6+3000*6.25/1e6=0.056+0.01875=0.07475
		// n=2: prefix=100000+(2-1)*3000+(2-1)*12000=115000
		//       call1: 115000*0.50/1e6+12000*6.25/1e6=0.0575+0.075=0.1325
		//       c=2: prefix=115000+12000+0=127000; 127000*0.50/1e6+3000*6.25/1e6=0.0635+0.01875=0.08225
		want: 0.4145,
	},
}

func Test_CumulativeCostAWarm(t *testing.T) {
	for _, tc := range costAWarmTests {
		t.Run(tc.name, func(t *testing.T) {
			got := CumulativeCostAWarm(tc.turns, tc.x, tc.sp, testPricing)
			if !approxEqual(got, tc.want) {
				t.Errorf("CumulativeCostAWarm(%d, %d) = %.10f, want %.10f", tc.turns, tc.x, got, tc.want)
			}
		})
	}
}

// CumulativeCostB — hand-derived expected values (overhead=40000, filteredTokens=50000, base=90000):
//
//	0 turns: setup only = 90000 * 6.25/1e6 = 0.5625
//	1 turn, K=1: 0.5625 + 90000*0.50/1e6 + 10000*6.25/1e6 = 0.5625+0.045+0.0625 = 0.67
//	2 turns, K=1: 0.67 + (90000+10000)*0.50/1e6 + 12000*6.25/1e6 = 0.67+0.05+0.075 = 0.795
var costBTests = []struct {
	name           string
	turns          int
	x              int
	filteredTokens int
	sp             CostParams
	want           float64
}{
	{
		name:           "GivenZeroTurns_ThenOnlySetupCost",
		turns:          0,
		x:              100000,
		filteredTokens: 50000,
		sp:             testParams,
		// setup = (overhead+filteredTokens) * CacheWrite/1e6 = 90000 * 6.25/1e6 = 0.5625
		want: 0.5625,
	},
	{
		name:           "GivenSingleTurnK1_ThenSetupPlusCacheReadPlusPromptWrite",
		turns:          1,
		x:              100000,
		filteredTokens: 50000,
		sp:             testParams,
		// setup=0.5625; n=1: 90000*0.50/1e6 + 10000*6.25/1e6 = 0.045+0.0625 = 0.1075
		want: 0.67,
	},
	{
		name:           "GivenTwoTurnsK1_ThenTurn2HasGrowthCacheWrite",
		turns:          2,
		x:              100000,
		filteredTokens: 50000,
		sp:             testParams,
		// 0.67 + (90000+10000)*0.50/1e6 + 12000*6.25/1e6 = 0.67+0.05+0.075 = 0.795
		want: 0.795,
	},
	{
		name:           "GivenTwoTurnsK2_ThenIntraTurnCallsApplied",
		turns:          2,
		x:              100000,
		filteredTokens: 50000,
		sp:             testParamsK2,
		// setup=0.5625
		// n=1: prefix=90000, crossTurnWrite=10000
		//   call1: 90000*0.50/1e6+10000*6.25/1e6=0.045+0.0625=0.1075
		//   j=2: prefix=90000+10000+0=100000; 100000*0.50/1e6+3000*6.25/1e6=0.05+0.01875=0.06875
		// n=2: prefix=(90000+10000)+(2-1)*3000+(2-2)*12000=103000, crossTurnWrite=12000
		//   call1: 103000*0.50/1e6+12000*6.25/1e6=0.0515+0.075=0.1265
		//   j=2: prefix=103000+12000+0=115000; 115000*0.50/1e6+3000*6.25/1e6=0.0575+0.01875=0.07625
		want: 0.9415,
	},
}

func Test_CumulativeCostB(t *testing.T) {
	for _, tc := range costBTests {
		t.Run(tc.name, func(t *testing.T) {
			got := CumulativeCostB(tc.turns, tc.x, tc.filteredTokens, tc.sp, testPricing)
			if !approxEqual(got, tc.want) {
				t.Errorf("CumulativeCostB(%d, %d, %d) = %.10f, want %.10f", tc.turns, tc.x, tc.filteredTokens, got, tc.want)
			}
		})
	}
}

func Test_CumulativeCostBWithInjectPages_GivenMultiplePages_ThenSetupCostsMoreThanOneShot(t *testing.T) {
	oneShot := CumulativeCostBWithInjectPages(0, 50000, 1, testParams, testPricing)
	multiPage := CumulativeCostBWithInjectPages(0, 50000, 5, testParams, testPricing)

	if !approxEqual(oneShot, 0.5625) {
		t.Fatalf("one-page setup cost = %.10f, want historical one-shot cost 0.5625", oneShot)
	}
	if !approxEqual(multiPage, 0.7125) {
		t.Fatalf("five-page setup cost = %.10f, want 0.7125", multiPage)
	}
	if multiPage <= oneShot {
		t.Fatalf("multi-page setup cost %.10f must exceed one-shot setup cost %.10f", multiPage, oneShot)
	}
}

func Test_CumulativeCostAWarm_GivenAnyTurns_AlwaysCheaperThanColdCostA(t *testing.T) {
	x := 100000
	for turns := 1; turns <= 20; turns++ {
		cold := CumulativeCostA(turns, x, testParams, testPricing)
		warm := CumulativeCostAWarm(turns, x, testParams, testPricing)
		if warm >= cold {
			t.Errorf("turns=%d: warm (%.10f) >= cold (%.10f); warm should always be cheaper", turns, warm, cold)
		}
	}
}

func Test_CumulativeCostA_GivenIncreasingTurns_ThenCostMonotonicallyIncreases(t *testing.T) {
	x := 100000
	prev := CumulativeCostA(1, x, testParams, testPricing)
	for turns := 2; turns <= 20; turns++ {
		curr := CumulativeCostA(turns, x, testParams, testPricing)
		if curr <= prev {
			t.Errorf("turns=%d: cost (%.10f) not greater than turns=%d cost (%.10f)", turns, curr, turns-1, prev)
		}
		prev = curr
	}
}

func Test_CumulativeCostAWarm_GivenIncreasingTurns_ThenCostMonotonicallyIncreases(t *testing.T) {
	x := 100000
	prev := CumulativeCostAWarm(1, x, testParams, testPricing)
	for turns := 2; turns <= 20; turns++ {
		curr := CumulativeCostAWarm(turns, x, testParams, testPricing)
		if curr <= prev {
			t.Errorf("turns=%d: cost (%.10f) not greater than turns=%d cost (%.10f)", turns, curr, turns-1, prev)
		}
		prev = curr
	}
}

func Test_CumulativeCostB_GivenSufficientTurns_ThenCheaperThanCostA(t *testing.T) {
	x := 100000
	filteredTokens := 50000 // base=90000 < x=100000 means compression happened
	found := false
	for turns := 1; turns <= 200; turns++ {
		costA := CumulativeCostA(turns, x, testParams, testPricing)
		costB := CumulativeCostB(turns, x, filteredTokens, testParams, testPricing)
		if costB < costA {
			found = true
			break
		}
	}
	if !found {
		t.Error("CostB never became cheaper than CostA within 200 turns, but filteredTokens < contextTokens")
	}
}

func Test_CumulativeCostB_GivenNoCompression_ThenNeverCheaperThanCostA(t *testing.T) {
	x := 100000
	filteredTokens := x // no compression: same as context
	for turns := 1; turns <= 200; turns++ {
		costA := CumulativeCostA(turns, x, testParams, testPricing)
		costB := CumulativeCostB(turns, x, filteredTokens, testParams, testPricing)
		if costB < costA {
			t.Errorf("turns=%d: CostB (%.10f) < CostA (%.10f) with no compression; setup cost should never be recovered", turns, costB, costA)
			return
		}
	}
}

func Test_ComputeCostMetrics_GivenInjectPages_ThenColdAndWarmMetricsMatchFixture(t *testing.T) {
	tests := []struct {
		name                 string
		injectPages          int
		wantBreakEven        int
		wantSaving10Pct      float64
		wantSaving100Pct     float64
		wantWarmBreakEven    int
		wantWarmSaving10Pct  float64
		wantWarmSaving100Pct float64
	}{
		{
			name:                 "OnePage",
			injectPages:          1,
			wantBreakEven:        1,
			wantSaving10Pct:      3.0142271521581656,
			wantSaving100Pct:     1.2012610310922243,
			wantWarmBreakEven:    92,
			wantWarmSaving10Pct:  -32.302631578947356,
			wantWarmSaving100Pct: 0.11611374407584504,
		},
		{
			name:                 "FivePages",
			injectPages:          5,
			wantBreakEven:        28,
			wantSaving10Pct:      -4.219918013021479,
			wantSaving100Pct:     0.8496724366262155,
			wantWarmBreakEven:    117,
			wantWarmSaving10Pct:  -42.17105263157893,
			wantWarmSaving100Pct: -0.23933649289099318,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Result{
				ContextTokens:  100000,
				FilteredTokens: 50000,
				CallsPerTurn:   1.0,
				ToolIOPerCall:  3000,
				AvgResponse:    2000,
				Prompt:         10000,
				InjectPages:    tt.injectPages,
			}

			ComputeCostMetrics(&r, 40000, testPricing)

			if r.BreakEven != tt.wantBreakEven {
				t.Errorf("BreakEven = %d, want %d", r.BreakEven, tt.wantBreakEven)
			}
			if !approxEqual(r.Saving10Pct, tt.wantSaving10Pct) {
				t.Errorf("Saving10Pct = %.15f, want %.15f", r.Saving10Pct, tt.wantSaving10Pct)
			}
			if !approxEqual(r.Saving100Pct, tt.wantSaving100Pct) {
				t.Errorf("Saving100Pct = %.15f, want %.15f", r.Saving100Pct, tt.wantSaving100Pct)
			}
			if r.WarmBreakEven != tt.wantWarmBreakEven {
				t.Errorf("WarmBreakEven = %d, want %d", r.WarmBreakEven, tt.wantWarmBreakEven)
			}
			if !approxEqual(r.WarmSaving10Pct, tt.wantWarmSaving10Pct) {
				t.Errorf("WarmSaving10Pct = %.15f, want %.15f", r.WarmSaving10Pct, tt.wantWarmSaving10Pct)
			}
			if !approxEqual(r.WarmSaving100Pct, tt.wantWarmSaving100Pct) {
				t.Errorf("WarmSaving100Pct = %.15f, want %.15f", r.WarmSaving100Pct, tt.wantWarmSaving100Pct)
			}
		})
	}
}

func Test_ComputeCostMetrics_GivenCompression_ThenBreakEvenExists(t *testing.T) {
	r := Result{
		ContextTokens:  100000,
		FilteredTokens: 50000, // filteredTokens < contextTokens → compression happened
		CallsPerTurn:   1.0,
		ToolIOPerCall:  3000,
		AvgResponse:    2000,
		Prompt:         10000,
	}
	ComputeCostMetrics(&r, 40000, testPricing)

	if r.BreakEven <= 0 {
		t.Errorf("BreakEven = %d, want > 0 when filteredTokens < contextTokens", r.BreakEven)
	}
	if r.Saving10Pct <= 0 {
		t.Errorf("Saving10Pct = %f, want > 0 when filteredTokens < contextTokens", r.Saving10Pct)
	}
	if r.Saving100Pct <= 0 {
		t.Errorf("Saving100Pct = %f, want > 0 when filteredTokens < contextTokens", r.Saving100Pct)
	}
}

func Test_ComputeCostMetrics_GivenCompression_ThenWarmBreakEvenAtLeastBreakEven(t *testing.T) {
	r := Result{
		ContextTokens:  100000,
		FilteredTokens: 50000,
		CallsPerTurn:   1.0,
		ToolIOPerCall:  3000,
		AvgResponse:    2000,
		Prompt:         10000,
	}
	ComputeCostMetrics(&r, 40000, testPricing)

	if r.WarmBreakEven != -1 && r.BreakEven != -1 && r.WarmBreakEven < r.BreakEven {
		t.Errorf("WarmBreakEven (%d) < BreakEven (%d); warm cache is harder to beat, so WarmBreakEven must be >= BreakEven", r.WarmBreakEven, r.BreakEven)
	}
}

func Test_ComputeCostMetrics_GivenNoCompression_ThenBreakEvenNeverReached(t *testing.T) {
	r := Result{
		ContextTokens:  100000,
		FilteredTokens: 100000, // no compression
		CallsPerTurn:   1.0,
		ToolIOPerCall:  3000,
		AvgResponse:    2000,
		Prompt:         10000,
	}
	ComputeCostMetrics(&r, 40000, testPricing)

	if r.BreakEven != -1 {
		t.Errorf("BreakEven = %d, want -1 when filteredTokens == contextTokens (no compression)", r.BreakEven)
	}
	if r.WarmBreakEven != -1 {
		t.Errorf("WarmBreakEven = %d, want -1 when filteredTokens == contextTokens (no compression)", r.WarmBreakEven)
	}
}

// Haiku 5.5 is billed per request: a request whose prompt (cache read + cache
// write) is over 100,000 tokens pays the higher rates, and every other request
// keeps the standard rates. Rates in $/M tokens:
//
//	prompt <= 100000: cache read 0.01, cache write 0.125
//	prompt >  100000: cache read 0.05, cache write 0.625
//
// All expected values are hand-derived from those rates. testParams has
// P=10000, g=12000, toolIO=3000; K=3 adds two tool round-trip calls per turn.
func Test_CumulativeCost_GivenTieredModel_ThenEachRequestIsBilledByItsOwnPromptSize(t *testing.T) {
	haiku := models.Haiku55
	k3 := testParams
	k3.K = 3.0
	k2dot5 := testParams
	k2dot5.K = 2.5

	tests := []struct {
		name string
		got  float64
		want float64
	}{
		// Cold existing session (CumulativeCostA)
		{
			name: "A_GivenEveryPromptBelowThreshold_ThenOnlyStandardRates",
			// turn1: 60000 write = 0.0075; turn2: read 60000 + write 12000 = 0.0006 + 0.0015
			got:  CumulativeCostA(2, 50000, testParams, haiku),
			want: 0.0096,
		},
		{
			name: "A_GivenEveryPromptAboveThreshold_ThenOnlyHigherRates",
			// turn1: 210000 write = 0.13125; turn2: read 210000 + write 12000 = 0.0105 + 0.0075
			got:  CumulativeCostA(2, 200000, testParams, haiku),
			want: 0.14925,
		},
		{
			name: "A_GivenPromptCrossesThresholdBetweenTurns_ThenEarlierTurnsKeepStandardRates",
			// turn1: 90000 write (standard) = 0.01125
			// turn2: read 90000 + write 12000 = 102000 (higher) = 0.0045 + 0.0075
			// turn3: read 102000 + write 12000 = 114000 (higher) = 0.0051 + 0.0075
			got:  CumulativeCostA(3, 80000, testParams, haiku),
			want: 0.03585,
		},
		{
			name: "A_GivenFirstWritePromptExactlyAtThreshold_ThenStandardRates",
			got:  CumulativeCostA(1, 90000, testParams, haiku),
			want: 0.0125, // 100000 * 0.125 / 1e6
		},
		{
			name: "A_GivenFirstWritePromptOneTokenOverThreshold_ThenHigherRates",
			got:  CumulativeCostA(1, 90001, testParams, haiku),
			want: 0.062500625, // 100001 * 0.625 / 1e6
		},
		{
			name: "A_GivenCacheReadAloneBelowThresholdButReadPlusWriteOver_ThenHigherRates",
			// turn1: 98000 write (standard) = 0.01225
			// turn2: read 98000 + write 12000 = 110000 (higher) = 0.0049 + 0.0075
			got:  CumulativeCostA(2, 88000, testParams, haiku),
			want: 0.02465,
		},
		{
			name: "A_GivenReadPlusWriteExactlyAtThreshold_ThenStandardRates",
			// turn1: 88000 write = 0.011; turn2: read 88000 + write 12000 = 100000 = 0.00088 + 0.0015
			got:  CumulativeCostA(2, 78000, testParams, haiku),
			want: 0.01338,
		},
		{
			name: "A_GivenToolCallsAllAboveThreshold_ThenHigherRatesForEveryCall",
			// call1: 210000 write = 0.13125
			// tool1: read 210000 + write 3000 = 0.0105 + 0.001875
			// tool2: read 213000 + write 3000 = 0.01065 + 0.001875
			got:  CumulativeCostA(1, 200000, k3, haiku),
			want: 0.15615,
		},
		{
			name: "A_GivenPromptCrossesThresholdBetweenToolCalls_ThenCallsBeforeCrossingKeepStandardRates",
			// call1: 95000 write (standard) = 0.011875
			// tool1: read 95000 + write 3000 = 98000 (standard) = 0.00095 + 0.000375
			// tool2: read 98000 + write 3000 = 101000 (higher) = 0.0049 + 0.001875
			got:  CumulativeCostA(1, 85000, k3, haiku),
			want: 0.019975,
		},
		{
			name: "A_GivenFractionalToolCallCrossesThreshold_ThenItIsWeightedAtHigherRates",
			// call1 = 0.011875; tool1 (standard) = 0.001325
			// fractional tool2: 0.5 * (read 98000 + write 3000 = 101000 (higher) = 0.0049 + 0.001875)
			got:  CumulativeCostA(1, 85000, k2dot5, haiku),
			want: 0.0165875,
		},

		// Warm existing session (CumulativeCostAWarm)
		{
			name: "AWarm_GivenReadPlusWriteExactlyAtThreshold_ThenStandardRates",
			got:  CumulativeCostAWarm(1, 88000, testParams, haiku),
			want: 0.00238, // read 88000 * 0.01 + write 12000 * 0.125, per M
		},
		{
			name: "AWarm_GivenReadPlusWriteOneTokenOverThreshold_ThenHigherRates",
			got:  CumulativeCostAWarm(1, 88001, testParams, haiku),
			want: 0.01190005, // read 88001 * 0.05 + write 12000 * 0.625, per M
		},
		{
			name: "AWarm_GivenPromptCrossesThresholdBetweenTurns_ThenEarlierTurnsKeepStandardRates",
			// turn1: read 80000 + write 12000 = 92000 (standard) = 0.0008 + 0.0015
			// turn2: read 92000 + write 12000 = 104000 (higher) = 0.0046 + 0.0075
			got:  CumulativeCostAWarm(2, 80000, testParams, haiku),
			want: 0.0144,
		},

		// New session with one-shot setup (CumulativeCostB)
		{
			name: "B_GivenSetupAndTurnPromptsExactlyAtThreshold_ThenStandardRates",
			// setup: 90000 write = 0.01125; turn1: read 90000 + write 10000 = 100000 = 0.0009 + 0.00125
			got:  CumulativeCostB(1, 0, 50000, testParams, haiku),
			want: 0.0134,
		},
		{
			name: "B_GivenSetupBelowThresholdAndTurnOneTokenOver_ThenOnlyTheTurnPaysHigherRates",
			// setup: 90001 write (standard) = 0.011250125
			// turn1: read 90001 + write 10000 = 100001 (higher) = 0.0045000500 + 0.00625
			got:  CumulativeCostB(1, 0, 50001, testParams, haiku),
			want: 0.022000175,
		},
		{
			name: "B_GivenSetupAboveThreshold_ThenSetupPaysHigherRates",
			got:  CumulativeCostB(0, 0, 70000, testParams, haiku),
			want: 0.06875, // 110000 * 0.625 / 1e6
		},

		// New session with paginated setup (CumulativeCostBWithInjectPages)
		{
			name: "BPages_GivenEveryRequestBelowThreshold_ThenOnlyStandardRates",
			// 40000 overhead write + five pages of 10000: reads 40000..80000 step 10000 (300000 in total)
			// reads 300000 * 0.01 + writes (40000 + 50000) * 0.125, per M
			got:  CumulativeCostBWithInjectPages(0, 50000, 5, testParams, haiku),
			want: 0.01425,
		},
		{
			name: "BPages_GivenEveryRequestAboveThreshold_ThenOnlyHigherRates",
			// overhead 120000, five pages of 10000: reads 120000..160000 step 10000
			// reads 700000 * 0.05 + writes (120000 + 50000) * 0.625, per M
			got: CumulativeCostBWithInjectPages(0, 50000, 5, CostParams{Overhead: 120000}, haiku),
			// 0.035 + 0.10625
			want: 0.14125,
		},
		{
			name: "BPages_GivenPromptCrossesThresholdDuringSetup_ThenOnlyLaterPagesPayHigherRates",
			// overhead 40000, five pages of 20000
			// overhead write: 40000 = 0.005
			// page0: read 40000 + write 20000 = 0.0004 + 0.0025
			// page1: read 60000 + write 20000 = 0.0006 + 0.0025
			// page2: read 80000 + write 20000 = 100000, standard = 0.0008 + 0.0025
			// page3: read 100000 + write 20000 = 120000, higher = 0.005 + 0.0125
			// page4: read 120000 + write 20000 = 140000, higher = 0.006 + 0.0125
			got:  CumulativeCostBWithInjectPages(0, 100000, 5, testParams, haiku),
			want: 0.0503,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !approxEqual(tt.got, tt.want) {
				t.Fatalf("cost = %.10f, want %.10f", tt.got, tt.want)
			}
		})
	}
}

func Test_CumulativeCost_GivenNonTieredModel_ThenPromptSizeDoesNotChangeRates(t *testing.T) {
	k3 := testParams
	k3.K = 3.0
	opus := models.Opus48 // $0.50 read, $6.25 write per M at every prompt size

	tests := []struct {
		name string
		got  float64
		want float64
	}{
		{
			name: "GivenPromptCrossesHundredThousandBetweenTurns",
			// turn1: 90000 write = 0.5625; turn2: read 90000 + write 12000 = 0.045 + 0.075
			got:  CumulativeCostA(2, 80000, testParams, opus),
			want: 0.6825,
		},
		{
			name: "GivenPromptCrossesHundredThousandBetweenToolCalls",
			// call1: 95000 write = 0.59375
			// tool1: read 95000 + write 3000 = 0.0475 + 0.01875
			// tool2: read 98000 + write 3000 = 0.049 + 0.01875
			got:  CumulativeCostA(1, 85000, k3, opus),
			want: 0.72775,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !approxEqual(tt.got, tt.want) {
				t.Fatalf("cost = %.10f, want %.10f", tt.got, tt.want)
			}
		})
	}
}
