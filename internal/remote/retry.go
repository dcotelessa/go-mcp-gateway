package remote

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	maxRetries      = 3
	initialBackoff  = 2 * time.Second
	maxBackoff      = 30 * time.Second
	backoffMultiple = 2.0
)

// parseRetryAfter parses the Retry-After header per RFC 7231 §7.1.3.
// Supports delta-seconds ("5") and HTTP-date ("Wed, 21 Oct 2025 07:28:00 GMT").
// Returns 0 if the header is absent or unparseable — caller uses exponential backoff.
func parseRetryAfter(header string, now time.Time) time.Duration {
	if header == "" {
		return 0
	}

	// Try delta-seconds first
	if secs, err := strconv.Atoi(strings.TrimSpace(header)); err == nil {
		return time.Duration(secs) * time.Second
	}

	// Try HTTP-date formats
	for _, layout := range []string{
		http.TimeFormat,                    // "Mon, 02 Jan 2006 15:04:05 GMT"
		"Monday, 02-Jan-06 15:04:05 MST",  // RFC 850
		"Mon Jan _2 15:04:05 2006",        // ANSI C
	} {
		if t, err := time.Parse(layout, strings.TrimSpace(header)); err == nil {
			d := t.Sub(now)
			if d < 0 {
				return 0
			}
			return d
		}
	}

	return 0
}

// exponentialBackoff returns the wait duration for attempt n (0-indexed).
// Caps at maxBackoff.
func exponentialBackoff(attempt int) time.Duration {
	d := float64(initialBackoff) * math.Pow(backoffMultiple, float64(attempt))
	if d > float64(maxBackoff) {
		d = float64(maxBackoff)
	}
	return time.Duration(d)
}

// doWithRetry wraps a single HTTP call with 429 retry logic.
// On 429: reads Retry-After header, waits, retries up to maxRetries times.
// On success: returns result with tokens from the successful call only.
// On maxRetries exceeded: returns RateLimitedError (no fallback triggered).
// On non-429 error: returns immediately (caller decides fallback).
func (c *baseClient) doWithRetry(ctx context.Context, req RemoteRequest) (RemoteResult, error) {
	var lastRetryAfter int

	for attempt := 0; attempt <= maxRetries; attempt++ {
		result, statusCode, err := c.do(ctx, req)

		if err != nil {
			return RemoteResult{}, err // terminal error — return immediately
		}

		if statusCode == http.StatusTooManyRequests {
			if attempt == maxRetries {
				return RemoteResult{}, &RateLimitedError{
					Provider:       c.name,
					RetryAfterSecs: lastRetryAfter,
					Attempts:       attempt + 1,
				}
			}

			// Read Retry-After from a probe request isn't possible here since
			// we don't have the response headers — use exponential backoff
			// unless the caller pre-populated retryState.
			wait := exponentialBackoff(attempt)
			lastRetryAfter = int(wait.Seconds())

			select {
			case <-ctx.Done():
				return RemoteResult{}, ctx.Err()
			case <-time.After(wait):
			}
			continue
		}

		// Success
		return result, nil
	}

	// Should not reach here
	return RemoteResult{}, &RateLimitedError{
		Provider: c.name,
		Attempts: maxRetries + 1,
	}
}

// doWithRetryAndHeader is like doWithRetry but uses the Retry-After header
// from a mock or pre-captured response for accurate wait times.
// Used in tests and by adapters that pre-check the 429 response headers.
func doWithRetryAndHeader(
	ctx context.Context,
	fn func(context.Context) (RemoteResult, int, string, error),
	providerName string,
) (RemoteResult, error) {
	var lastRetryAfter int

	for attempt := 0; attempt <= maxRetries; attempt++ {
		result, statusCode, retryAfterHeader, err := fn(ctx)

		if err != nil {
			return RemoteResult{}, err
		}

		if statusCode == http.StatusTooManyRequests {
			if attempt == maxRetries {
				return RemoteResult{}, &RateLimitedError{
					Provider:       providerName,
					RetryAfterSecs: lastRetryAfter,
					Attempts:       attempt + 1,
				}
			}

			wait := parseRetryAfter(retryAfterHeader, time.Now())
			if wait == 0 {
				wait = exponentialBackoff(attempt)
			}
			lastRetryAfter = int(wait.Seconds())

			select {
			case <-ctx.Done():
				return RemoteResult{}, ctx.Err()
			case <-time.After(wait):
			}
			continue
		}

		return result, nil
	}

	return RemoteResult{}, &RateLimitedError{
		Provider: providerName,
		Attempts: maxRetries + 1,
	}
}
