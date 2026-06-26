package benchmark

import (
	"math"
	"testing"
)

const epsilon = 1e-9

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
			got:  CumulativeCostA(1, 100000, testParamsK4478, PricingOpus),
			want: 0.9506535,
		},
		{
			name: "WarmExistingSession",
			got:  CumulativeCostAWarm(1, 100000, testParamsK4478, PricingOpus),
			want: 0.3916315,
		},
		{
			name: "NewSession",
			got:  CumulativeCostB(1, 100000, 50000, testParamsK4478, PricingOpus),
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
			got := CumulativeCostA(tc.turns, tc.x, tc.sp, PricingOpus)
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
			got := CumulativeCostAWarm(tc.turns, tc.x, tc.sp, PricingOpus)
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
			got := CumulativeCostB(tc.turns, tc.x, tc.filteredTokens, tc.sp, PricingOpus)
			if !approxEqual(got, tc.want) {
				t.Errorf("CumulativeCostB(%d, %d, %d) = %.10f, want %.10f", tc.turns, tc.x, tc.filteredTokens, got, tc.want)
			}
		})
	}
}

func Test_CumulativeCostBWithInjectPages_GivenMultiplePages_ThenSetupCostsMoreThanOneShot(t *testing.T) {
	oneShot := CumulativeCostBWithInjectPages(0, 100000, 50000, 1, testParams, PricingOpus)
	multiPage := CumulativeCostBWithInjectPages(0, 100000, 50000, 5, testParams, PricingOpus)

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
		cold := CumulativeCostA(turns, x, testParams, PricingOpus)
		warm := CumulativeCostAWarm(turns, x, testParams, PricingOpus)
		if warm >= cold {
			t.Errorf("turns=%d: warm (%.10f) >= cold (%.10f); warm should always be cheaper", turns, warm, cold)
		}
	}
}

func Test_CumulativeCostA_GivenIncreasingTurns_ThenCostMonotonicallyIncreases(t *testing.T) {
	x := 100000
	prev := CumulativeCostA(1, x, testParams, PricingOpus)
	for turns := 2; turns <= 20; turns++ {
		curr := CumulativeCostA(turns, x, testParams, PricingOpus)
		if curr <= prev {
			t.Errorf("turns=%d: cost (%.10f) not greater than turns=%d cost (%.10f)", turns, curr, turns-1, prev)
		}
		prev = curr
	}
}

func Test_CumulativeCostAWarm_GivenIncreasingTurns_ThenCostMonotonicallyIncreases(t *testing.T) {
	x := 100000
	prev := CumulativeCostAWarm(1, x, testParams, PricingOpus)
	for turns := 2; turns <= 20; turns++ {
		curr := CumulativeCostAWarm(turns, x, testParams, PricingOpus)
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
		costA := CumulativeCostA(turns, x, testParams, PricingOpus)
		costB := CumulativeCostB(turns, x, filteredTokens, testParams, PricingOpus)
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
		costA := CumulativeCostA(turns, x, testParams, PricingOpus)
		costB := CumulativeCostB(turns, x, filteredTokens, testParams, PricingOpus)
		if costB < costA {
			t.Errorf("turns=%d: CostB (%.10f) < CostA (%.10f) with no compression; setup cost should never be recovered", turns, costB, costA)
			return
		}
	}
}

func Test_ComputeCostMetrics_GivenMultiPageInject_ThenBreakEvenLaterAndSavingsLower(t *testing.T) {
	oneShot := Result{
		ContextTokens:  100000,
		FilteredTokens: 50000,
		CallsPerTurn:   1.0,
		ToolIOPerCall:  3000,
		AvgResponse:    2000,
		Prompt:         10000,
		InjectPages:    1,
	}
	multiPage := oneShot
	multiPage.InjectPages = 5

	ComputeCostMetrics(&oneShot, 40000, PricingOpus)
	ComputeCostMetrics(&multiPage, 40000, PricingOpus)

	if oneShot.BreakEven <= 0 {
		t.Fatalf("one-shot fixture must have finite break-even, got %d", oneShot.BreakEven)
	}
	if multiPage.BreakEven <= oneShot.BreakEven {
		t.Fatalf("multi-page break-even = %d, want later than one-shot break-even %d", multiPage.BreakEven, oneShot.BreakEven)
	}
	if multiPage.Saving10Pct >= oneShot.Saving10Pct {
		t.Fatalf("multi-page 10-turn saving = %.10f, want less than one-shot %.10f", multiPage.Saving10Pct, oneShot.Saving10Pct)
	}
	if multiPage.Saving100Pct >= oneShot.Saving100Pct {
		t.Fatalf("multi-page 100-turn saving = %.10f, want less than one-shot %.10f", multiPage.Saving100Pct, oneShot.Saving100Pct)
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
	ComputeCostMetrics(&r, 40000, PricingOpus)

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
	ComputeCostMetrics(&r, 40000, PricingOpus)

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
	ComputeCostMetrics(&r, 40000, PricingOpus)

	if r.BreakEven != -1 {
		t.Errorf("BreakEven = %d, want -1 when filteredTokens == contextTokens (no compression)", r.BreakEven)
	}
	if r.WarmBreakEven != -1 {
		t.Errorf("WarmBreakEven = %d, want -1 when filteredTokens == contextTokens (no compression)", r.WarmBreakEven)
	}
}
