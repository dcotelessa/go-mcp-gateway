package router

import (
	"fmt"
)

// DefaultRouter is the concrete implementation of Router.
type DefaultRouter struct{}

// New returns a DefaultRouter ready to use.
func New() *DefaultRouter {
	return &DefaultRouter{}
}

// complexityToTier is the single source of truth for task routing.
// Matches the complexity enum semantics in task-execution-workflow.ts.
var complexityToTier = map[Complexity]Tier{
	ComplexityScaffold:   TierLocalOrnith,
	ComplexitySingleFile: TierLocalOrnith,
	ComplexityRecovery:   TierLocalOrnith,
	ComplexityTextOp:     TierLocalQwen,
	ComplexityMultiFile:  TierRemoteDeepSeek,
}

// fallbackChain defines the cascade order for remote tier exhaustion.
// Local tiers are terminal — no further fallback.
var fallbackChain = map[Tier]Tier{
	TierRemoteGLM:      TierRemoteDeepSeek,
	TierRemoteDeepSeek: TierLocalOrnith,
}

// validComplexities is the set of accepted complexity values.
var validComplexities = map[Complexity]struct{}{
	ComplexityScaffold:   {},
	ComplexitySingleFile: {},
	ComplexityMultiFile:  {},
	ComplexityRecovery:   {},
	ComplexityTextOp:     {},
}

// Route resolves a Complexity to a Tier.
// If forceTier is non-empty it overrides the complexity mapping and appends
// a "forced_tier" reasoning tag.
// Returns an error if complexity is not in the allowed enum.
func (r *DefaultRouter) Route(complexity Complexity, forceTier Tier) (RouteResult, error) {
	if _, ok := validComplexities[complexity]; !ok {
		return RouteResult{}, fmt.Errorf("router: unknown complexity %q", complexity)
	}

	if forceTier != "" {
		return RouteResult{
			Tier:          forceTier,
			ReasoningTags: []string{"forced_tier"},
		}, nil
	}

	tier, ok := complexityToTier[complexity]
	if !ok {
		return RouteResult{}, fmt.Errorf("router: no tier mapping for complexity %q", complexity)
	}

	return RouteResult{Tier: tier}, nil
}

// RouteFallback returns the next tier in the cascade chain after fromTier.
// Returns (nextTier, true) if a fallback exists, ("", false) if terminal.
func (r *DefaultRouter) RouteFallback(fromTier Tier) (Tier, bool) {
	next, ok := fallbackChain[fromTier]
	return next, ok
}

// Classify maps a task description to a Complexity and QALevel.
// Uses keyword heuristics — sufficient for the classification task
// which runs on a cheap local model in production.
func (r *DefaultRouter) Classify(task string) (ClassifyResult, error) {
	if task == "" {
		return ClassifyResult{}, fmt.Errorf("router: classify requires non-empty task")
	}
	complexity, qaLevel := classifyTask(task)
	return ClassifyResult{
		Complexity: complexity,
		QALevel:    qaLevel,
	}, nil
}

// Interpret maps vitest output + diff + scenarios to a QAVerdict.
// Production path calls GLM-4.7 local via the model manager;
// this implementation parses structured vitest output directly.
func (r *DefaultRouter) Interpret(vitestOutput, diff string, scenarios []string) (QAVerdict, error) {
	verdict := interpretOutput(vitestOutput, diff, scenarios)
	return verdict, nil
}
