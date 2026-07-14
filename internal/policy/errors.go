package policy

import "fmt"

// ErrRateLimited is returned when a session exceeds its per-minute request rate.
type ErrRateLimited struct {
	Session           string
	RetryAfterSeconds int
}

func (e *ErrRateLimited) Error() string {
	return fmt.Sprintf("policy: rate limited session %q — retry after %ds",
		e.Session, e.RetryAfterSeconds)
}

// ReasoningTags returns the Mastra workflow contract tags for this error.
func (e *ErrRateLimited) ReasoningTags() []string {
	return []string{"used_token_bucket", "added_header_429", "rate_limited"}
}

// ErrSessionTokenBudgetExhausted is returned when a session exceeds
// its per-hour token budget.
type ErrSessionTokenBudgetExhausted struct {
	Session           string
	RetryAfterSeconds int
}

func (e *ErrSessionTokenBudgetExhausted) Error() string {
	return fmt.Sprintf("policy: token budget exhausted session %q — retry after %ds",
		e.Session, e.RetryAfterSeconds)
}

// ReasoningTags returns the Mastra workflow contract tags for this error.
func (e *ErrSessionTokenBudgetExhausted) ReasoningTags() []string {
	return []string{"used_token_bucket", "added_header_429", "token_budget_exhausted"}
}

// ErrTierBudgetExhausted is returned when a tier's daily token budget
// is exhausted.
type ErrTierBudgetExhausted struct {
	Tier string
}

func (e *ErrTierBudgetExhausted) Error() string {
	return fmt.Sprintf("policy: tier budget exhausted for %q", e.Tier)
}

// ReasoningTags returns the Mastra workflow contract tags for this error.
func (e *ErrTierBudgetExhausted) ReasoningTags() []string {
	return []string{"used_token_bucket", "added_header_429", "tier_budget_exhausted"}
}
