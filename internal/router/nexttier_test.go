package router

import "testing"

// RETRY-7 precondition — a middle tier escalates one step.
func TestNextTierUp_MiddleTier(t *testing.T) {
	next, ok := NextTierUp(string(TierRemoteDeepSeek))
	if !ok || next != string(TierRemoteGLM) {
		t.Errorf("NextTierUp(deepseek) = %q, %v; want remote_glm, true", next, ok)
	}
}

// RETRY-7 precondition — the top of the ladder has no next tier.
func TestNextTierUp_TopTierHasNoNext(t *testing.T) {
	for _, tier := range []Tier{TierRemoteGLM, TierRemoteOpus} {
		if next, ok := NextTierUp(string(tier)); ok {
			t.Errorf("NextTierUp(%s) = %q; want no escalation", tier, next)
		}
	}
}

// Local tiers escalate straight to remote, not to each other.
func TestNextTierUp_LocalGoesRemote(t *testing.T) {
	for _, tier := range []Tier{TierLocalOrnith, TierLocalQwen} {
		next, ok := NextTierUp(string(tier))
		if !ok || next != string(TierRemoteDeepSeek) {
			t.Errorf("NextTierUp(%s) = %q, %v; want remote_deepseek", tier, next, ok)
		}
	}
}

func TestNextTierUp_UnknownTier(t *testing.T) {
	if _, ok := NextTierUp("not_a_tier"); ok {
		t.Error("unknown tier should not escalate")
	}
}

// Walking the ladder from any tier never reaches Opus and always ends.
func TestNextTierUp_NeverReachesOpusAndTerminates(t *testing.T) {
	all := []Tier{TierLocalOrnith, TierLocalQwen, TierRemoteDeepSeek, TierRemoteGLM, TierRemoteOpus}
	for _, start := range all {
		tier := string(start)
		for steps := 0; ; steps++ {
			if steps > len(all) {
				t.Fatalf("ladder from %s does not terminate", start)
			}
			next, ok := NextTierUp(tier)
			if !ok {
				break
			}
			if next == string(TierRemoteOpus) {
				t.Fatalf("ladder from %s escalated into remote_opus", start)
			}
			tier = next
		}
	}
}
