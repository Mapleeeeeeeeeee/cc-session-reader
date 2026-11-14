package analyzer

import (
	"math"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

func assistantEventWithUsage(usage *session.Usage) session.Event {
	return session.Event{
		Kind:      session.EventAssistantMessage,
		Assistant: &session.AssistantMessage{Text: "reply", Usage: usage},
	}
}

func TestStatsResult_OneHourCacheWriteShare(t *testing.T) {
	// A response that Claude Code logged as two consecutive records (streaming
	// partial + completion) carries one identical usage and must count once.
	loggedTwice := &session.Usage{
		InputTokens: 2, CacheReadInputTokens: 9000, OutputTokens: 40,
		CacheCreationInputTokens: 800, CacheWrite5mTokens: 200, CacheWrite1hTokens: 600,
	}
	tests := []struct {
		name      string
		events    []session.Event
		wantShare float64
		wantOK    bool
	}{
		{
			name: "GivenEveryWriteIsOneHour_ThenShareIsOne",
			events: []session.Event{
				assistantEventWithUsage(&session.Usage{InputTokens: 1, CacheCreationInputTokens: 500, CacheWrite1hTokens: 500}),
			},
			wantShare: 1, wantOK: true,
		},
		{
			name: "GivenEveryWriteIsFiveMinute_ThenShareIsZeroNotUnknown",
			events: []session.Event{
				assistantEventWithUsage(&session.Usage{InputTokens: 1, CacheCreationInputTokens: 500, CacheWrite5mTokens: 500}),
			},
			wantShare: 0, wantOK: true,
		},
		{
			// 1h = 600 + 400 = 1000 and 5m = 200 + 100 = 300, so 1000 / 1300.
			// Counting the logged-twice response twice would give 1600 / 1900.
			name: "GivenResponseLoggedTwiceAndAnotherResponse_ThenCountsEachResponseOnce",
			events: []session.Event{
				assistantEventWithUsage(loggedTwice),
				assistantEventWithUsage(loggedTwice),
				assistantEventWithUsage(&session.Usage{InputTokens: 3, CacheCreationInputTokens: 500, CacheWrite5mTokens: 100, CacheWrite1hTokens: 400}),
			},
			wantShare: 1000.0 / 1300.0, wantOK: true,
		},
		{
			// 200 of the 1000 written tokens have no TTL and count as 5-minute writes: 800 / 1000.
			name: "GivenPartOfTheWriteHasNoTTL_ThenItCountsAsFiveMinute",
			events: []session.Event{
				assistantEventWithUsage(&session.Usage{InputTokens: 1, CacheCreationInputTokens: 1000, CacheWrite1hTokens: 800}),
			},
			wantShare: 0.8, wantOK: true,
		},
		{
			name: "GivenWritesWithoutAnyTTLBreakdown_ThenShareIsUnknown",
			events: []session.Event{
				assistantEventWithUsage(&session.Usage{InputTokens: 1, CacheCreationInputTokens: 500, CacheReadInputTokens: 100}),
			},
			wantOK: false,
		},
		{
			name:   "GivenNoUsage_ThenShareIsUnknown",
			events: []session.Event{{Kind: session.EventAssistantMessage, Assistant: &session.AssistantMessage{Text: "no usage"}}},
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			share, ok := ComputeStats(tt.events).OneHourCacheWriteShare()
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if math.Abs(share-tt.wantShare) > 1e-12 {
				t.Errorf("share = %v, want %v", share, tt.wantShare)
			}
		})
	}
}
