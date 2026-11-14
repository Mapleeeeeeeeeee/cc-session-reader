package benchmark

import (
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/models"
)

func withOneHourShare(sp CostParams, share float64) CostParams {
	sp.OneHourWriteShare = share
	return sp
}

// Cache write rates in $/M tokens, from the official price list:
//
//	Opus 4.8 (testPricing): 5-minute 6.25, 1-hour 10.00
//	Haiku 5.5, prompt <= 100000: 5-minute 0.125, 1-hour 0.20; cache read 0.01
//	Haiku 5.5, prompt >  100000: 5-minute 0.625, 1-hour 1.00; cache read 0.05
//
// A share is the fraction of every cache write billed at the 1-hour rate, so a
// share of 0.5 on Opus 4.8 writes at (6.25 + 10.00) / 2 = 8.125. All expected
// values are hand-derived from those rates. testParams has P=10000, g=12000,
// toolIO=3000; K=3 adds two tool round-trip calls per turn.
func Test_CumulativeCost_GivenOneHourWriteShare_ThenCacheWritesUseTheBlendedRate(t *testing.T) {
	opus := testPricing
	haiku := models.Haiku55
	k3 := testParams
	k3.K = 3.0

	tests := []struct {
		name string
		got  float64
		want float64
	}{
		// Cold existing session (CumulativeCostA), non-tiered model
		{
			name: "A_GivenAllOneHour_ThenEveryWriteIsBilledAtOneHourRate",
			// turn1: write 90000 = 0.9; turn2: read 90000 * 0.50 + write 12000 * 10.00 = 0.045 + 0.12
			got:  CumulativeCostA(2, 80000, withOneHourShare(testParams, 1), opus),
			want: 1.065,
		},
		{
			name: "A_GivenHalfOneHour_ThenEveryWriteIsBilledAtTheAverageRate",
			// turn1: write 90000 * 8.125 = 0.73125; turn2: read 0.045 + write 12000 * 8.125 = 0.0975
			got:  CumulativeCostA(2, 80000, withOneHourShare(testParams, 0.5), opus),
			want: 0.87375,
		},
		{
			name: "A_GivenAllFiveMinute_ThenEveryWriteIsBilledAtFiveMinuteRate",
			// turn1: write 90000 * 6.25 = 0.5625; turn2: read 0.045 + write 12000 * 6.25 = 0.075
			got:  CumulativeCostA(2, 80000, withOneHourShare(testParams, 0), opus),
			want: 0.6825,
		},
		{
			name: "A_GivenAllOneHourAndToolCalls_ThenToolIOWritesAreBilledAtOneHourRate",
			// call1: write 95000 * 10.00 = 0.95
			// tool1: read 95000 * 0.50 + write 3000 * 10.00 = 0.0475 + 0.03
			// tool2: read 98000 * 0.50 + write 3000 * 10.00 = 0.049 + 0.03
			got:  CumulativeCostA(1, 85000, withOneHourShare(k3, 1), opus),
			want: 1.1065,
		},

		// Warm existing session (CumulativeCostAWarm)
		{
			name: "AWarm_GivenAllOneHour_ThenGrowthWriteIsBilledAtOneHourRate",
			// read 88000 * 0.50 + write 12000 * 10.00 = 0.044 + 0.12
			got:  CumulativeCostAWarm(1, 88000, withOneHourShare(testParams, 1), opus),
			want: 0.164,
		},

		// New session (CumulativeCostB, CumulativeCostBWithInjectPages)
		{
			name: "B_GivenAllOneHour_ThenSetupWriteIsBilledAtOneHourRate",
			// setup: write 90000 * 10.00 = 0.9; turn1: read 90000 * 0.50 + write 10000 * 10.00 = 0.045 + 0.1
			got:  CumulativeCostB(1, 0, 50000, withOneHourShare(testParams, 1), opus),
			want: 1.045,
		},
		{
			name: "BPages_GivenAllOneHourAndOneTier_ThenSetupWritesAreBilledAtOneHourRate",
			// 40000 overhead write + five pages of 10000: reads 300000 * 0.50 + writes 90000 * 10.00
			got:  CumulativeCostBWithInjectPages(0, 50000, 5, withOneHourShare(testParams, 1), opus),
			want: 1.05,
		},

		// Haiku 5.5: each request keeps its own tier, and each tier has its own 1-hour rate.
		{
			name: "A_GivenHaikuBelowThresholdAndAllOneHour_ThenStandardOneHourRate",
			got:  CumulativeCostA(1, 85000, withOneHourShare(testParams, 1), haiku),
			want: 0.019, // write 95000 * 0.20
		},
		{
			name: "A_GivenHaikuOverThresholdAndAllOneHour_ThenHigherOneHourRate",
			got:  CumulativeCostA(1, 95000, withOneHourShare(testParams, 1), haiku),
			want: 0.105, // write 105000 * 1.00
		},
		{
			name: "A_GivenHaikuOverThresholdAndHalfOneHour_ThenAverageOfHigherTierRates",
			got:  CumulativeCostA(1, 95000, withOneHourShare(testParams, 0.5), haiku),
			want: 0.0853125, // write 105000 * (0.625 + 1.00) / 2
		},
		{
			name: "A_GivenHaikuPromptCrossesThresholdBetweenTurnsAndAllOneHour_ThenEachTurnUsesItsTierRates",
			// turn1: write 98000 (standard) * 0.20 = 0.0196
			// turn2: read 98000 + write 12000 = 110000 (higher) = 98000 * 0.05 + 12000 * 1.00 = 0.0049 + 0.012
			got:  CumulativeCostA(2, 88000, withOneHourShare(testParams, 1), haiku),
			want: 0.0365,
		},
		{
			name: "A_GivenHaikuPromptCrossesThresholdBetweenToolCallsAndAllOneHour_ThenCallsKeepTheirOwnTierRates",
			// call1: write 95000 (standard) * 0.20 = 0.019
			// tool1: read 95000 + write 3000 = 98000 (standard) = 0.00095 + 0.0006
			// tool2: read 98000 + write 3000 = 101000 (higher) = 0.0049 + 0.003
			got:  CumulativeCostA(1, 85000, withOneHourShare(k3, 1), haiku),
			want: 0.02845,
		},
		{
			name: "A_GivenHaikuPromptCrossesThresholdBetweenToolCallsAndHalfOneHour_ThenCallsKeepTheirOwnTierRates",
			// standard write rate (0.125 + 0.20) / 2 = 0.1625, higher (0.625 + 1.00) / 2 = 0.8125
			// call1: write 95000 * 0.1625 = 0.0154375
			// tool1: read 0.00095 + write 3000 * 0.1625 = 0.0004875
			// tool2: read 0.0049 + write 3000 * 0.8125 = 0.0024375
			got:  CumulativeCostA(1, 85000, withOneHourShare(k3, 0.5), haiku),
			want: 0.0242125,
		},
		{
			name: "BPages_GivenHaikuPromptCrossesThresholdDuringSetupAndAllOneHour_ThenLaterPagesPayHigherOneHourRate",
			// overhead 40000, five pages of 20000
			// overhead write: 40000 * 0.20 = 0.008
			// page0: read 40000 * 0.01 + write 20000 * 0.20 = 0.0004 + 0.004
			// page1: read 60000 * 0.01 + write 20000 * 0.20 = 0.0006 + 0.004
			// page2: read 80000 + write 20000 = 100000, standard = 0.0008 + 0.004
			// page3: read 100000 + write 20000 = 120000, higher = 0.005 + 0.02
			// page4: read 120000 + write 20000 = 140000, higher = 0.006 + 0.02
			got:  CumulativeCostBWithInjectPages(0, 100000, 5, withOneHourShare(testParams, 1), haiku),
			want: 0.0728,
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

// Warm: the existing session already holds its context in cache, so staying
// pays almost no cache write, while opening a new session pays one large
// write. A dearer write therefore lifts the new session's cost and pushes
// break-even later.
func Test_ComputeCostMetrics_GivenOneHourWrites_ThenWarmBreakEvenComesLaterThanWithFiveMinuteWrites(t *testing.T) {
	newResult := func(oneHourShare float64) Result {
		r := Result{
			ContextTokens:     300000,
			FilteredTokens:    20000,
			CallsPerTurn:      1.0,
			ToolIOPerCall:     3000,
			AvgResponse:       2000,
			Prompt:            10000,
			OneHourWriteShare: oneHourShare,
		}
		ComputeCostMetrics(&r, 40000, testPricing)
		return r
	}

	fiveMinute := newResult(0)
	oneHour := newResult(1)

	if fiveMinute.WarmBreakEven <= 0 || oneHour.WarmBreakEven <= 0 {
		t.Fatalf("warm break-even must exist for both TTLs to compare, got 5m=%d 1h=%d", fiveMinute.WarmBreakEven, oneHour.WarmBreakEven)
	}
	if oneHour.WarmBreakEven <= fiveMinute.WarmBreakEven {
		t.Errorf("WarmBreakEven 1h = %d, want later than 5m = %d", oneHour.WarmBreakEven, fiveMinute.WarmBreakEven)
	}
	if oneHour.WarmSaving10Pct >= fiveMinute.WarmSaving10Pct {
		t.Errorf("WarmSaving10Pct 1h = %.2f, want below 5m = %.2f", oneHour.WarmSaving10Pct, fiveMinute.WarmSaving10Pct)
	}
}
