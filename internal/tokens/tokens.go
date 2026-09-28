// Package tokens provides token counting via the Anthropic API.
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

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/config"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/models"
)

const (
	countTokensURL = "https://api.anthropic.com/v1/messages/count_tokens"
	apiVersion     = "2023-06-01"

	// perAttemptTimeout bounds a single HTTP attempt. Each retry gets a fresh
	// budget, so a slow first attempt does not starve subsequent retries.
	perAttemptTimeout = 30 * time.Second

	// Retry policy for transient failures (429, 5xx, network errors).
	maxAttempts    = 3
	baseRetryDelay = 500 * time.Millisecond
)

// DefaultCountTokensModel is the model used when none is specified.
var DefaultCountTokensModel = models.DefaultCountTokens.ID

// CountTokensAPI calls the Anthropic count_tokens endpoint.
// Resolves the API key from: env ANTHROPIC_API_KEY → config file path in
// ~/.claude/skills/<skillDirName>/config.json → error.
func CountTokensAPI(text string) (int, error) {
	counter, err := NewCounter(DefaultCountTokensModel)
	if err != nil {
		return 0, err
	}
	return counter.Count(text)
}

// Counter counts tokens with a resolved API key and reusable HTTP client.
type Counter struct {
	apiKey   string
	endpoint string
	model    string
	client   *http.Client
}

// NewCounter resolves credentials once and returns a reusable token counter.
func NewCounter(model string) (*Counter, error) {
	apiKey := resolveAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set (set env var or configure anthropic_api_key_file in ~/.claude/skills/cc-session/config.json)")
	}
	if model == "" {
		model = DefaultCountTokensModel
	}
	return &Counter{
		apiKey:   apiKey,
		endpoint: countTokensURL,
		model:    model,
		client:   &http.Client{},
	}, nil
}

// Count returns the Anthropic input token count for text.
func (c *Counter) Count(text string) (int, error) {
	return countTokens(text, c.apiKey, c.endpoint, c.model, c.client)
}

func resolveAPIKey() string {
	cfg := config.Get()
	if key := cfg.AnthropicAPIKey(); key != "" {
		return key
	}
	if cfg.AnthropicAPIKeyFile == "" {
		return ""
	}
	return readKeyFromFile(cfg.AnthropicAPIKeyFile)
}

func readKeyFromFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("#")) || !bytes.Contains(line, []byte("=")) {
			continue
		}
		k, v, _ := bytes.Cut(line, []byte("="))
		if string(bytes.TrimSpace(k)) == "ANTHROPIC_API_KEY" {
			return string(bytes.TrimSpace(v))
		}
	}
	return ""
}

func countTokens(text string, apiKey string, endpoint string, model string, client *http.Client) (int, error) {
	// ctx bounds the whole call (e.g. caller cancellation) but is NOT a single
	// shared deadline for every attempt — each attempt derives its own
	// perAttemptTimeout, and the retry loop is bounded by maxAttempts.
	ctx := context.Background()

	payload := map[string]any{
		"model": model,
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
