package policy

import (
	"sync"
	"time"
)

// PolicyConfig holds the runtime limits for the policy layer.
type PolicyConfig struct {
	RateLimitPerMin      int
	TokenLimitPerHour    int
	SessionIdleTTLMin    int
	SweepIntervalMin     int
	BudgetDeepSeekDaily  int
	BudgetGLMDaily       int
}

// DefaultPolicyConfig returns sensible defaults matching config.example.yaml.
func DefaultPolicyConfig() PolicyConfig {
	return PolicyConfig{
		RateLimitPerMin:     20,
		TokenLimitPerHour:   200000,
		SessionIdleTTLMin:   30,
		SweepIntervalMin:    5,
		BudgetDeepSeekDaily: 1000000,
		BudgetGLMDaily:      500000,
	}
}

// Registry manages per-session token buckets and per-tier budgets.
type Registry struct {
	cfg     PolicyConfig
	mu      sync.Mutex
	sessions map[string]*TokenBucket
	tiers   map[string]*TierBudget
	done    chan struct{}
}

// New creates a Registry and starts the background sweep goroutine.
func New(cfg PolicyConfig) *Registry {
	r := &Registry{
		cfg:      cfg,
		sessions: make(map[string]*TokenBucket),
		tiers:    make(map[string]*TierBudget),
		done:     make(chan struct{}),
	}

	// Initialize known remote tier budgets
	r.tiers["remote_deepseek"] = newTierBudget("remote_deepseek", cfg.BudgetDeepSeekDaily)
	r.tiers["remote_glm"] = newTierBudget("remote_glm", cfg.BudgetGLMDaily)

	go r.sweepLoop()
	return r
}

// getOrCreateSession returns the bucket for sessionID, creating if needed.
func (r *Registry) getOrCreateSession(sessionID string) *TokenBucket {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.sessions[sessionID]; ok {
		return b
	}
	b := newTokenBucket(r.cfg.RateLimitPerMin, r.cfg.TokenLimitPerHour)
	r.sessions[sessionID] = b
	return b
}

// CheckSession validates rate and token limits for a session.
// Returns a typed error with ReasoningTags() if rejected.
func (r *Registry) CheckSession(sessionID string) error {
	b := r.getOrCreateSession(sessionID)
	now := time.Now()

	if err := b.CheckRate(now); err != nil {
		if e, ok := err.(*ErrRateLimited); ok {
			e.Session = sessionID
		}
		return err
	}

	if err := b.CheckTokens(now); err != nil {
		if e, ok := err.(*ErrSessionTokenBudgetExhausted); ok {
			e.Session = sessionID
		}
		return err
	}

	return nil
}

// DeductSession records token usage for a session after completion.
func (r *Registry) DeductSession(sessionID string, tokens int) {
	b := r.getOrCreateSession(sessionID)
	b.Deduct(time.Now(), tokens)
}

// CheckTier validates the daily budget for a remote tier.
func (r *Registry) CheckTier(tier string) error {
	r.mu.Lock()
	b, ok := r.tiers[tier]
	r.mu.Unlock()
	if !ok {
		return nil // local tiers have no budget tracking
	}
	return b.Check()
}

// DeductTier records token usage against a tier's daily budget.
func (r *Registry) DeductTier(tier string, tokens int) {
	r.mu.Lock()
	b, ok := r.tiers[tier]
	r.mu.Unlock()
	if !ok {
		return
	}
	b.Deduct(tokens)
}

// MarkUpstreamRateLimit signals that a remote tier returned 429.
func (r *Registry) MarkUpstreamRateLimit(tier string, retryAfter time.Duration) {
	r.mu.Lock()
	b, ok := r.tiers[tier]
	r.mu.Unlock()
	if !ok {
		return
	}
	b.MarkUpstreamRateLimit(retryAfter)
}

// TierRemaining returns the remaining token budget for a tier.
// Returns -1 for local tiers (slot-based, not token-based).
func (r *Registry) TierRemaining(tier string) int64 {
	r.mu.Lock()
	b, ok := r.tiers[tier]
	r.mu.Unlock()
	if !ok {
		return -1
	}
	return b.Remaining()
}

// SessionCount returns the number of active sessions.
func (r *Registry) SessionCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sessions)
}

// Stop shuts down the background sweep goroutine.
func (r *Registry) Stop() {
	close(r.done)
}

// sweepLoop runs periodic cleanup of idle sessions and tier pruning.
func (r *Registry) sweepLoop() {
	ticker := time.NewTicker(time.Duration(r.cfg.SweepIntervalMin) * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.sweep()
		case <-r.done:
			return
		}
	}
}

// sweep evicts idle sessions and prunes tier rolling windows.
func (r *Registry) sweep() {
	now := time.Now()
	idleCutoff := now.Add(-time.Duration(r.cfg.SessionIdleTTLMin) * time.Minute)

	r.mu.Lock()
	for id, b := range r.sessions {
		if b.IdleSince().Before(idleCutoff) {
			delete(r.sessions, id)
		}
	}
	tiers := make([]*TierBudget, 0, len(r.tiers))
	for _, b := range r.tiers {
		tiers = append(tiers, b)
	}
	r.mu.Unlock()

	for _, b := range tiers {
		b.Prune(now)
	}
}
