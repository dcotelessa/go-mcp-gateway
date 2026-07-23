package remote

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRetryAfter_DeltaSeconds(t *testing.T) {
	now := time.Now()
	d := parseRetryAfter("5", now)
	assert.Equal(t, 5*time.Second, d)
}

func TestParseRetryAfter_HTTPDate(t *testing.T) {
	now := time.Now()
	future := now.Add(10 * time.Second).UTC()
	header := future.Format(http.TimeFormat)
	d := parseRetryAfter(header, now)
	assert.InDelta(t, 10.0, d.Seconds(), 1.0)
}

func TestParseRetryAfter_Empty(t *testing.T) {
	d := parseRetryAfter("", time.Now())
	assert.Equal(t, time.Duration(0), d)
}

func TestParseRetryAfter_Invalid(t *testing.T) {
	d := parseRetryAfter("not-a-date-or-number", time.Now())
	assert.Equal(t, time.Duration(0), d)
}

func TestParseRetryAfter_PastDate(t *testing.T) {
	now := time.Now()
	past := now.Add(-10 * time.Second).UTC()
	header := past.Format(http.TimeFormat)
	d := parseRetryAfter(header, now)
	assert.Equal(t, time.Duration(0), d)
}

func TestExponentialBackoff(t *testing.T) {
	assert.Equal(t, 2*time.Second, exponentialBackoff(0))
	assert.Equal(t, 4*time.Second, exponentialBackoff(1))
	assert.Equal(t, 8*time.Second, exponentialBackoff(2))
}

func TestExponentialBackoff_Cap(t *testing.T) {
	// Large attempt should cap at maxBackoff
	assert.Equal(t, maxBackoff, exponentialBackoff(100))
}

func TestDoWithRetryAndHeader_Success(t *testing.T) {
	calls := 0
	fn := func(_ context.Context) (RemoteResult, int, string, error) {
		calls++
		return RemoteResult{Content: "ok", PromptTokens: 10}, http.StatusOK, "", nil
	}

	result, err := doWithRetryAndHeader(context.Background(), fn, "test")
	require.NoError(t, err)
	assert.Equal(t, "ok", result.Content)
	assert.Equal(t, 1, calls)
}

func TestDoWithRetryAndHeader_429ThenSuccess(t *testing.T) {
	calls := 0
	fn := func(_ context.Context) (RemoteResult, int, string, error) {
		calls++
		if calls < 2 {
			return RemoteResult{}, http.StatusTooManyRequests, "1", nil
		}
		return RemoteResult{Content: "ok", PromptTokens: 10}, http.StatusOK, "", nil
	}

	result, err := doWithRetryAndHeader(context.Background(), fn, "test")
	require.NoError(t, err)
	assert.Equal(t, "ok", result.Content)
	assert.Equal(t, 2, calls)
}

func TestDoWithRetryAndHeader_MaxRetriesExceeded(t *testing.T) {
	calls := 0
	fn := func(_ context.Context) (RemoteResult, int, string, error) {
		calls++
		return RemoteResult{}, http.StatusTooManyRequests, "1", nil
	}

	_, err := doWithRetryAndHeader(context.Background(), fn, "test")
	require.Error(t, err)

	var rateLimited *RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	assert.Equal(t, "test", rateLimited.Provider)
	assert.Equal(t, maxRetries+1, calls)
}

func TestDoWithRetryAndHeader_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled immediately

	fn := func(_ context.Context) (RemoteResult, int, string, error) {
		return RemoteResult{}, http.StatusTooManyRequests, "60", nil
	}

	_, err := doWithRetryAndHeader(ctx, fn, "test")
	assert.Error(t, err)
}

func TestDoWithRetryAndHeader_TerminalError(t *testing.T) {
	calls := 0
	fn := func(_ context.Context) (RemoteResult, int, string, error) {
		calls++
		return RemoteResult{}, 0, "", &TerminalError{Provider: "test", StatusCode: 500}
	}

	_, err := doWithRetryAndHeader(context.Background(), fn, "test")
	require.Error(t, err)
	assert.Equal(t, 1, calls, "terminal errors must not retry")
}
