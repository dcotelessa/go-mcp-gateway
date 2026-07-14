package policy

import (
	"sync"
	"time"
)

// TokenBucket tracks per-session rate limiting and token usage
// using sliding windows (not fixed windows).
type TokenBucket struct {
	mu sync.Mutex

	// Rate limiting — sliding window of request timestamps
	rateLimitPerMin int
	requestTimes    []time.Time

	// Token budget — sliding window of token deductions
	tokenLimitPerHour int
	tokenDeductions   []tokenDeduction

	lastSeen time.Time
}

// tokenDeduction records a single token spend event.
type tokenDeduction struct {
	at     time.Time
	tokens int
}

// newTokenBucket creates a bucket with the given limits.
func newTokenBucket(rateLimitPerMin, tokenLimitPerHour int) *TokenBucket {
	return &TokenBucket{
		rateLimitPerMin:   rateLimitPerMin,
		tokenLimitPerHour: tokenLimitPerHour,
		lastSeen:          time.Now(),
	}
}

// CheckRate returns an error if the session has exceeded its per-minute
// request rate. Call before processing a request.
func (b *TokenBucket) CheckRate(now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.lastSeen = now
	cutoff := now.Add(-time.Minute)

	// Prune expired entries
	valid := b.requestTimes[:0]
	for _, t := range b.requestTimes {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	b.requestTimes = valid

	if len(b.requestTimes) >= b.rateLimitPerMin {
		retryAfter := int(time.Until(b.requestTimes[0].Add(time.Minute)).Seconds()) + 1
		return &ErrRateLimited{RetryAfterSeconds: retryAfter}
	}

	b.requestTimes = append(b.requestTimes, now)
	return nil
}

// CheckTokens returns an error if the session has exceeded its per-hour
// token budget. Call before dispatching to a model.
func (b *TokenBucket) CheckTokens(now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	total := b.hourlyTokens(now)
	if total >= b.tokenLimitPerHour {
		oldest := b.oldestDeduction()
		retryAfter := int(time.Until(oldest.Add(time.Hour)).Seconds()) + 1
		return &ErrSessionTokenBudgetExhausted{RetryAfterSeconds: retryAfter}
	}
	return nil
}

// Deduct records a token spend after a successful completion.
func (b *TokenBucket) Deduct(now time.Time, tokens int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokenDeductions = append(b.tokenDeductions, tokenDeduction{at: now, tokens: tokens})
}

// hourlyTokens sums token deductions in the last hour (caller holds lock).
func (b *TokenBucket) hourlyTokens(now time.Time) int {
	cutoff := now.Add(-time.Hour)
	total := 0
	valid := b.tokenDeductions[:0]
	for _, d := range b.tokenDeductions {
		if d.at.After(cutoff) {
			valid = append(valid, d)
			total += d.tokens
		}
	}
	b.tokenDeductions = valid
	return total
}

// oldestDeduction returns the timestamp of the oldest hourly deduction
// (caller holds lock). Returns zero time if none.
func (b *TokenBucket) oldestDeduction() time.Time {
	if len(b.tokenDeductions) == 0 {
		return time.Time{}
	}
	return b.tokenDeductions[0].at
}

// IdleSince returns when the bucket was last used.
func (b *TokenBucket) IdleSince() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastSeen
}
