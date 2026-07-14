package policy

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfig() PolicyConfig {
	return PolicyConfig{
		RateLimitPerMin:     20,
		TokenLimitPerHour:   200000,
		SessionIdleTTLMin:   30,
		SweepIntervalMin:    60,
		BudgetDeepSeekDaily: 1000000,
		BudgetGLMDaily:      500000,
	}
}

// --- Rate limiting ---

func TestCheckSession_RateLimit(t *testing.T) {
	cfg := testConfig()
	cfg.RateLimitPerMin = 3
	r := New(cfg)
	defer r.Stop()

	for i := 0; i < 3; i++ {
		require.NoError(t, r.CheckSession("s1"))
	}

	err := r.CheckSession("s1")
	require.Error(t, err)

	var rateLimited *ErrRateLimited
	require.ErrorAs(t, err, &rateLimited)
	assert.Equal(t, "s1", rateLimited.Session)
	assert.Greater(t, rateLimited.RetryAfterSeconds, 0)
	assert.Contains(t, rateLimited.ReasoningTags(), "added_header_429")
	assert.Contains(t, rateLimited.ReasoningTags(), "rate_limited")
	assert.Contains(t, rateLimited.ReasoningTags(), "used_token_bucket")
}

func TestCheckSession_RateLimit_SlidingWindow(t *testing.T) {
	cfg := testConfig()
	cfg.RateLimitPerMin = 2
	r := New(cfg)
	defer r.Stop()

	require.NoError(t, r.CheckSession("s1"))
	require.NoError(t, r.CheckSession("s1"))

	var rateLimited *ErrRateLimited
	err := r.CheckSession("s1")
	assert.ErrorAs(t, err, &rateLimited)
}

// --- Token budget ---

func TestCheckSession_TokenBudget(t *testing.T) {
	cfg := testConfig()
	cfg.TokenLimitPerHour = 100
	r := New(cfg)
	defer r.Stop()

	r.DeductSession("s1", 95)
	require.NoError(t, r.CheckSession("s1"))

	r.DeductSession("s1", 10)
	err := r.CheckSession("s1")
	require.Error(t, err)

	var exhausted *ErrSessionTokenBudgetExhausted
	require.ErrorAs(t, err, &exhausted)
	assert.Equal(t, "s1", exhausted.Session)
	assert.Contains(t, exhausted.ReasoningTags(), "added_header_429")
	assert.Contains(t, exhausted.ReasoningTags(), "token_budget_exhausted")
}

func TestDeductSession_RecordsUsage(t *testing.T) {
	r := New(testConfig())
	defer r.Stop()

	r.DeductSession("s1", 500)
	err := r.CheckSession("s1")
	assert.NoError(t, err)
}

// --- Tier budget ---

func TestCheckTier_Exhausted(t *testing.T) {
	cfg := testConfig()
	cfg.BudgetDeepSeekDaily = 100
	r := New(cfg)
	defer r.Stop()

	r.DeductTier("remote_deepseek", 101)

	err := r.CheckTier("remote_deepseek")
	require.Error(t, err)

	var exhausted *ErrTierBudgetExhausted
	require.ErrorAs(t, err, &exhausted)
	assert.Equal(t, "remote_deepseek", exhausted.Tier)
	assert.Contains(t, exhausted.ReasoningTags(), "tier_budget_exhausted")
}

func TestCheckTier_LocalTier_NoTracking(t *testing.T) {
	r := New(testConfig())
	defer r.Stop()

	assert.NoError(t, r.CheckTier("local_ornith"))
	assert.NoError(t, r.CheckTier("local_qwen"))
}

func TestDeductTier_Atomic(t *testing.T) {
	r := New(testConfig())
	defer r.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.DeductTier("remote_deepseek", 100)
		}()
	}
	wg.Wait()

	remaining := r.TierRemaining("remote_deepseek")
	assert.Equal(t, int64(990000), remaining)
}

// --- Upstream 429 ---

func TestMarkUpstreamRateLimit(t *testing.T) {
	r := New(testConfig())
	defer r.Stop()

	assert.Greater(t, r.TierRemaining("remote_deepseek"), int64(0))
	r.MarkUpstreamRateLimit("remote_deepseek", 100*time.Millisecond)
	assert.Equal(t, int64(0), r.TierRemaining("remote_deepseek"))
}

func TestUpstream429_NoTokenDeduction(t *testing.T) {
	cfg := testConfig()
	cfg.BudgetDeepSeekDaily = 1000
	r := New(cfg)
	defer r.Stop()

	initial := r.TierRemaining("remote_deepseek")
	r.MarkUpstreamRateLimit("remote_deepseek", 50*time.Millisecond)
	assert.Equal(t, int64(0), r.TierRemaining("remote_deepseek"))

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, initial, r.TierRemaining("remote_deepseek"),
		"upstream 429 should not permanently deduct from budget")
}

// --- Session eviction ---

func TestSweep_EvictsIdleSessions(t *testing.T) {
	cfg := testConfig()
	cfg.SessionIdleTTLMin = 0
	r := New(cfg)
	defer r.Stop()

	require.NoError(t, r.CheckSession("idle-session"))
	assert.Equal(t, 1, r.SessionCount())

	r.sweep()
	assert.Equal(t, 0, r.SessionCount())
}

// --- Concurrent safety ---

func TestCheckSession_Concurrent(t *testing.T) {
	r := New(testConfig())
	defer r.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.CheckSession("concurrent-session")
		}()
	}
	wg.Wait()
}

// --- Tier rolling window ---

func TestTierBudget_Prune(t *testing.T) {
	b := newTierBudget("remote_deepseek", 1000)
	b.Deduct(500)

	b.Prune(time.Now().Add(25 * time.Hour))
	assert.Equal(t, int64(1000), b.Remaining())
}
