package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPolicyPackageCompiles(t *testing.T) {
	cfg := DefaultPolicyConfig()
	r := New(cfg)
	defer r.Stop()

	assert.NotNil(t, r)
	assert.Equal(t, 20, cfg.RateLimitPerMin)
	assert.Equal(t, 200000, cfg.TokenLimitPerHour)
}

func TestErrorTypes(t *testing.T) {
	e1 := &ErrRateLimited{Session: "s1", RetryAfterSeconds: 30}
	assert.Contains(t, e1.Error(), "rate limited")
	assert.Contains(t, e1.ReasoningTags(), "added_header_429")
	assert.Contains(t, e1.ReasoningTags(), "rate_limited")

	e2 := &ErrSessionTokenBudgetExhausted{Session: "s1", RetryAfterSeconds: 60}
	assert.Contains(t, e2.Error(), "token budget exhausted")
	assert.Contains(t, e2.ReasoningTags(), "added_header_429")
	assert.Contains(t, e2.ReasoningTags(), "token_budget_exhausted")

	e3 := &ErrTierBudgetExhausted{Tier: "remote_deepseek"}
	assert.Contains(t, e3.Error(), "tier budget exhausted")
	assert.Contains(t, e3.ReasoningTags(), "tier_budget_exhausted")
}

func TestRegistry_SessionAutoRegistration(t *testing.T) {
	r := New(DefaultPolicyConfig())
	defer r.Stop()

	// First call auto-creates session
	assert.Equal(t, 0, r.SessionCount())
	err := r.CheckSession("new-session")
	assert.NoError(t, err)
	assert.Equal(t, 1, r.SessionCount())
}

func TestRegistry_TierRemaining_LocalTier(t *testing.T) {
	r := New(DefaultPolicyConfig())
	defer r.Stop()

	// Local tiers return -1 (slot-based, not token-based)
	assert.Equal(t, int64(-1), r.TierRemaining("local_ornith"))
	assert.Equal(t, int64(-1), r.TierRemaining("local_qwen"))
}

func TestRegistry_TierRemaining_RemoteTier(t *testing.T) {
	r := New(DefaultPolicyConfig())
	defer r.Stop()

	remaining := r.TierRemaining("remote_deepseek")
	assert.Equal(t, int64(1000000), remaining)
}
