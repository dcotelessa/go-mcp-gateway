package router

// escalation maps a tier to the next more capable tier, used when a model's
// output could not be parsed even with a stricter prompt.
//
// This is deliberately separate from fallbackChain. The fallback chain
// answers "what is cheaper when a budget runs out"; escalation answers
// "what is more capable when output fails." They point in opposite
// directions, and deriving one from the other would silently couple two
// independent policies.
//
// The ladder stops at remote_glm. remote_opus is never reached
// automatically: escalating a formatting failure into the most expensive
// tier is a cost runaway, and Opus is reserved for explicit force_tier use.
// Both local tiers escalate straight to remote: moving between local
// models is a VRAM swap, not an increase in capability.
var escalation = map[Tier]Tier{
	TierLocalOrnith:    TierRemoteDeepSeek,
	TierLocalQwen:      TierRemoteDeepSeek,
	TierRemoteDeepSeek: TierRemoteGLM,
}

// NextTierUp returns the tier to escalate to after tier fails, and false
// when tier is already at the top of the automatic ladder.
//
// It takes and returns plain strings so callers such as the filewriter
// executor can use it without importing this package.
func NextTierUp(tier string) (string, bool) {
	next, ok := escalation[Tier(tier)]
	if !ok {
		return "", false
	}
	return string(next), true
}
