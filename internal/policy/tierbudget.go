package policy

import (
	"sync/atomic"
	"time"
)

// TierBudget tracks daily token usage for a remote API tier
// using a 24-hour rolling window.
type TierBudget struct {
	tier       string
	dailyLimit int64

	mu          *int64 // atomic remaining counter
	deductions  []tierDeduction
	deductMu    chan struct{} // mutex via channel
}

// tierDeduction records a single tier-level token spend.
type tierDeduction struct {
	at     time.Time
	tokens int64
}

// newTierBudget creates a TierBudget with the given daily token limit.
func newTierBudget(tier string, dailyLimit int) *TierBudget {
	remaining := int64(dailyLimit)
	mu := make(chan struct{}, 1)
	mu <- struct{}{}
	return &TierBudget{
		tier:       tier,
		dailyLimit: int64(dailyLimit),
		mu:         &remaining,
		deductMu:   mu,
	}
}

// Check returns ErrTierBudgetExhausted if the tier has no remaining budget.
func (b *TierBudget) Check() error {
	if atomic.LoadInt64(b.mu) <= 0 {
		return &ErrTierBudgetExhausted{Tier: b.tier}
	}
	return nil
}

// Deduct atomically subtracts tokens from the tier's remaining budget
// and records the deduction for rolling window reset.
func (b *TierBudget) Deduct(tokens int) {
	atomic.AddInt64(b.mu, -int64(tokens))

	// Record for rolling window
	select {
	case <-b.deductMu:
		b.deductions = append(b.deductions, tierDeduction{
			at:     time.Now(),
			tokens: int64(tokens),
		})
		b.deductMu <- struct{}{}
	default:
	}
}

// Prune removes deductions older than 24 hours and recomputes remaining.
func (b *TierBudget) Prune(now time.Time) {
	select {
	case <-b.deductMu:
		cutoff := now.Add(-24 * time.Hour)
		var kept []tierDeduction
		var spent int64
		for _, d := range b.deductions {
			if d.at.After(cutoff) {
				kept = append(kept, d)
				spent += d.tokens
			}
		}
		b.deductions = kept
		remaining := b.dailyLimit - spent
		atomic.StoreInt64(b.mu, remaining)
		b.deductMu <- struct{}{}
	default:
	}
}

// Remaining returns the current remaining token count.
func (b *TierBudget) Remaining() int64 {
	return atomic.LoadInt64(b.mu)
}

// MarkUpstreamRateLimit temporarily zeros the remaining budget
// for the given duration to force cascade to next tier.
func (b *TierBudget) MarkUpstreamRateLimit(retryAfter time.Duration) {
	atomic.StoreInt64(b.mu, 0)
	// Restore after retry window — non-blocking
	go func() {
		time.Sleep(retryAfter)
		b.Prune(time.Now())
	}()
}
