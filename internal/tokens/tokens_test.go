package tokens

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{
			// 11 ASCII chars, ~0.25 tokens each -> heuristic approximation: 2
			name: "pure ASCII",
			text: "hello world",
			want: 2,
		},
		{
			name: "pure CJK",
			text: "你好世界",
			want: 6,
		},
		{
			name: "mixed ASCII and CJK",
			text: "hello 你好",
			want: 4,
		},
		{
			name: "empty string",
			text: "",
			want: 0,
		},
		{
			// 100 ASCII chars at ~0.25 tokens each -> 25
			name: "longer ASCII text",
			text: strings.Repeat("a", 100),
			want: 25,
		},
		{
			name: "longer CJK text",
			text: strings.Repeat("测", 10),
			want: 15,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokens(tt.text)
			if got != tt.want {
				t.Errorf("EstimateTokens(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}

// A single CJK char is ~1.5 tokens; integer conversion floors to 1.
func TestEstimateTokens_SingleCJKChar(t *testing.T) {
	got := EstimateTokens("世")
	want := 1
	if got != want {
		t.Errorf("single CJK char: got %d, want %d", got, want)
	}
}

// CJK Extension A range (U+3400-U+4DBF) should be detected as CJK.
func TestEstimateTokens_CJKExtensionA(t *testing.T) {
	// U+3400 "㐀" is CJK Extension A, same weight as unified CJK -> 1 after flooring.
	got := EstimateTokens("㐀")
	want := 1
	if got != want {
		t.Errorf("CJK Extension A char: got %d, want %d", got, want)
	}
}

// Non-CJK Unicode (accented Latin) should NOT be counted as CJK.
func TestEstimateTokens_NonCJKUnicode(t *testing.T) {
	// "café" = 4 visible characters, no CJK -> heuristic approximation: 1
	text := "café"
	got := EstimateTokens(text)
	want := 1
	if got != want {
		t.Errorf("accented text: got %d, want %d", got, want)
	}
}

func TestCountTokensAPI_WhenAPIKeyIsMissing_ThenReturnsError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := CountTokensAPI("hello")
	if err == nil {
		t.Fatal("CountTokensAPI returned nil error, want missing key error")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY not set") {
		t.Fatalf("error = %v, want missing key", err)
	}
}

func TestCountTokens_SendsAnthropicRequestAndParsesResponse(t *testing.T) {
	var sawRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q, want test-key", got)
		}
		if got := r.Header.Get("anthropic-version"); got != apiVersion {
			t.Fatalf("anthropic-version = %q, want %q", got, apiVersion)
		}

		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != anthropicModel {
			t.Fatalf("model = %q, want %q", payload.Model, anthropicModel)
		}
		if len(payload.Messages) != 1 || payload.Messages[0].Role != "user" || payload.Messages[0].Content != "hello" {
			t.Fatalf("messages = %#v, want one user hello message", payload.Messages)
		}

		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"input_tokens":42}`))
	}))
	defer server.Close()

	got, err := countTokens("hello", "test-key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("countTokens returned error: %v", err)
	}
	if !sawRequest {
		t.Fatal("server did not receive request")
	}
	if got != 42 {
		t.Fatalf("countTokens = %d, want 42", got)
	}
}

func TestCountTokens_WhenAPIReturnsError_ThenIncludesStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := countTokens("hello", "test-key", server.URL, server.Client())
	if err == nil {
		t.Fatal("countTokens returned nil error, want API status error")
	}
	if !strings.Contains(err.Error(), "API returned status 401") {
		t.Fatalf("error = %v, want status 401", err)
	}
}

// A 5xx is transient: the client must retry and ultimately succeed, returning
// the token count from the successful response. Guards the retry loop in
// countTokens — if retry were removed, this would surface the first 503 error.
func TestCountTokens_WhenTransientErrorThenSuccess_ThenRetriesAndSucceeds(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requestCount, 1)
		if n == 1 {
			http.Error(w, "overloaded", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"input_tokens":17}`))
	}))
	defer server.Close()

	got, err := countTokens("hello", "test-key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("countTokens returned error after retry: %v", err)
	}
	if got != 17 {
		t.Fatalf("countTokens = %d, want 17 from the successful retry", got)
	}
	if n := atomic.LoadInt32(&requestCount); n != 2 {
		t.Fatalf("request count = %d, want 2 (one failure + one retry)", n)
	}
}

// A 4xx (except 429) is a client error and must NOT be retried — retrying a
// malformed/unauthorized request only wastes attempts. Guards isRetryable's
// non-transient classification: a regression that retried 400s would show count > 1.
func TestCountTokens_WhenNonTransientError_ThenDoesNotRetry(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	_, err := countTokens("hello", "test-key", server.URL, server.Client())
	if err == nil {
		t.Fatal("countTokens returned nil error, want immediate 400 error")
	}
	if !strings.Contains(err.Error(), "API returned status 400") {
		t.Fatalf("error = %v, want status 400", err)
	}
	if n := atomic.LoadInt32(&requestCount); n != 1 {
		t.Fatalf("request count = %d, want 1 (no retry on a non-transient 400)", n)
	}
}

// 429 is transient and the Retry-After header dictates the wait. The client must
// retry after honoring the hint and then succeed. Guards both the 429 branch in
// attemptCountTokens and that parseRetryAfter's delay is actually applied.
func TestCountTokens_When429WithRetryAfter_ThenRespectsHintAndSucceeds(t *testing.T) {
	const retryAfterSeconds = 1
	var requestCount int32
	var firstAt, secondAt time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requestCount, 1)
		if n == 1 {
			firstAt = time.Now()
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		secondAt = time.Now()
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"input_tokens":9}`))
	}))
	defer server.Close()

	got, err := countTokens("hello", "test-key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("countTokens returned error after 429 retry: %v", err)
	}
	if got != 9 {
		t.Fatalf("countTokens = %d, want 9 from the retry", got)
	}
	if n := atomic.LoadInt32(&requestCount); n != 2 {
		t.Fatalf("request count = %d, want 2 (429 then success)", n)
	}
	// The wait must be at least the Retry-After hint (1s), not the 500ms default
	// backoff — proving the header was honored. Allow scheduling slack on the upper bound.
	waited := secondAt.Sub(firstAt)
	if waited < retryAfterSeconds*time.Second {
		t.Fatalf("waited %v between attempts, want >= %ds (Retry-After honored)", waited, retryAfterSeconds)
	}
}

// waitBeforeRetry must abort the backoff sleep the moment the context is done,
// returning ctx.Err() instead of sleeping out the full delay. Guards the
// select-on-ctx.Done() path against a regression to an unconditional time.Sleep.
func TestWaitBeforeRetry_WhenContextCancelled_ThenReturnsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the call

	start := time.Now()
	// attempt=3 -> backoff would be 500ms<<2 = 2s if not aborted.
	err := waitBeforeRetry(ctx, 3, 0)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("waitBeforeRetry returned nil, want ctx.Err()")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("waitBeforeRetry took %v, want near-instant return on cancelled context", elapsed)
	}
}

func TestParseRetryAfter_DerivesDurationFromHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{
			// No header -> caller falls back to exponential backoff.
			name:   "empty header yields zero",
			header: "",
			want:   0,
		},
		{
			name:   "valid delay-seconds",
			header: "5",
			want:   5 * time.Second,
		},
		{
			// Negative seconds are nonsensical -> treated as no hint.
			name:   "negative seconds yields zero",
			header: "-3",
			want:   0,
		},
		{
			// HTTP-date form is not delay-seconds; parsing must fall back to
			// backoff (zero) rather than misinterpret it. Pins behavior that
			// was previously only incidentally correct (untested).
			name:   "http-date form falls back to zero",
			header: "Wed, 21 Oct 2015 07:28:00 GMT",
			want:   0,
		},
		{
			// Non-numeric garbage -> no hint.
			name:   "non-numeric yields zero",
			header: "soon",
			want:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRetryAfter(tt.header)
			if got != tt.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.header, got, tt.want)
			}
		})
	}
}
