// Package tokens provides token counting via Anthropic API and heuristic estimation.
package tokens

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	countTokensURL = "https://api.anthropic.com/v1/messages/count_tokens"
	anthropicModel = "claude-sonnet-4-6"
	apiVersion     = "2023-06-01"

	// perAttemptTimeout bounds a single HTTP attempt. Each retry gets a fresh
	// budget, so a slow first attempt does not starve subsequent retries.
	perAttemptTimeout = 30 * time.Second

	// Retry policy for transient failures (429, 5xx, network errors).
	maxAttempts    = 3
	baseRetryDelay = 500 * time.Millisecond
)

// CJK Unicode ranges for token estimation.
const (
	cjkUnifiedStart    = 0x4E00
	cjkUnifiedEnd      = 0x9FFF
	cjkExtAStart       = 0x3400
	cjkExtAEnd         = 0x4DBF
	cjkTokenMultiplier = 1.5
	asciiTokenRatio    = 0.25
)

// EstimateTokens provides a rough token count using character-class heuristics.
// CJK characters are weighted at ~1.5 tokens each; other characters at ~0.25.
// This deliberately undercounts single-character inputs due to int truncation,
// which is acceptable for aggregated context-budget estimates.
func EstimateTokens(text string) int {
	cjkCount := 0
	otherCount := 0
	for _, ch := range text {
		if (ch >= cjkUnifiedStart && ch <= cjkUnifiedEnd) ||
			(ch >= cjkExtAStart && ch <= cjkExtAEnd) {
			cjkCount++
		} else {
			otherCount++
		}
	}
	return int(float64(cjkCount)*cjkTokenMultiplier + float64(otherCount)*asciiTokenRatio)
}

// CountTokensAPI calls the Anthropic count_tokens endpoint.
// Returns (tokenCount, nil) on success, or (0, error) on failure.
// Requires ANTHROPIC_API_KEY environment variable.
func CountTokensAPI(text string) (int, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return 0, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	// The per-attempt context (set inside countTokens) owns request deadlines,
	// so the client carries no global Timeout that would also cap the retry loop.
	return countTokens(text, apiKey, countTokensURL, &http.Client{})
}

func countTokens(text string, apiKey string, endpoint string, client *http.Client) (int, error) {
	// ctx bounds the whole call (e.g. caller cancellation) but is NOT a single
	// shared deadline for every attempt — each attempt derives its own
	// perAttemptTimeout, and the retry loop is bounded by maxAttempts.
	ctx := context.Background()

	payload := map[string]any{
		"model": anthropicModel,
		"messages": []map[string]any{
			{"role": "user", "content": text},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		count, retryAfter, err := attemptCountTokens(ctx, body, apiKey, endpoint, client)
		if err == nil {
			return count, nil
		}
		lastErr = err
		if !isRetryable(err) || attempt == maxAttempts {
			return 0, err
		}
		if err := waitBeforeRetry(ctx, attempt, retryAfter); err != nil {
			return 0, err
		}
	}
	return 0, lastErr
}

// transientError marks an error as eligible for retry (429, 5xx, network).
type transientError struct {
	err error
}

func (e *transientError) Error() string { return e.err.Error() }
func (e *transientError) Unwrap() error { return e.err }

func isRetryable(err error) bool {
	var t *transientError
	return errors.As(err, &t)
}

// attemptCountTokens performs one HTTP attempt. It returns a retryAfter hint
// (parsed from the Retry-After header) when the server provides one.
func attemptCountTokens(ctx context.Context, body []byte, apiKey, endpoint string, client *http.Client) (int, time.Duration, error) {
	// Each attempt gets a fresh deadline so a slow attempt cannot consume the
	// budget that subsequent retries depend on.
	attemptCtx, cancel := context.WithTimeout(ctx, perAttemptTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set("content-type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		// Network/transport errors are transient.
		return 0, 0, &transientError{fmt.Errorf("http request: %w", err)}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, &transientError{fmt.Errorf("read response: %w", err)}
	}

	if resp.StatusCode != http.StatusOK {
		statusErr := fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return 0, parseRetryAfter(resp.Header.Get("Retry-After")), &transientError{statusErr}
		}
		return 0, 0, statusErr
	}

	var result struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, 0, fmt.Errorf("parse response: %w", err)
	}
	return result.InputTokens, 0, nil
}

// waitBeforeRetry sleeps for the backoff interval (honoring a Retry-After hint
// when present), aborting early if the context is cancelled.
func waitBeforeRetry(ctx context.Context, attempt int, retryAfter time.Duration) error {
	delay := retryAfter
	if delay <= 0 {
		// Exponential backoff: base * 2^(attempt-1).
		delay = baseRetryDelay << (attempt - 1)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// parseRetryAfter reads a Retry-After header given as delay-seconds.
// HTTP-date form is uncommon for this endpoint and is ignored (falls back to backoff).
func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	seconds, err := strconv.Atoi(header)
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
