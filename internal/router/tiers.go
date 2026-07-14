package router

// Tier identifies a model backend — local or remote.
type Tier string

const (
	TierLocalOrnith  Tier = "local_ornith"
	TierLocalQwen    Tier = "local_qwen"
	TierRemoteDeepSeek Tier = "remote_deepseek"
	TierRemoteGLM    Tier = "remote_glm"
)

// Complexity maps to a task type from the Mastra workflow contract.
// These values must match exactly what task-execution-workflow.ts emits.
type Complexity string

const (
	ComplexityScaffold   Complexity = "scaffold"
	ComplexitySingleFile Complexity = "single_file"
	ComplexityMultiFile  Complexity = "multi_file"
	ComplexityRecovery   Complexity = "recovery"
	ComplexityTextOp     Complexity = "text_op"
)

// QALevel controls how much QA runs after implementation.
type QALevel string

const (
	QALevelSkip      QALevel = "skip"
	QALevelTypeCheck QALevel = "typecheck"
	QALevelFull      QALevel = "full"
)

// RouteResult is returned by Route() describing the resolved tier
// and any reasoning tags to append to the Mastra workflow contract.
type RouteResult struct {
	Tier          Tier
	ReasoningTags []string
}

// ClassifyResult is returned by Classify() for the REST /classify endpoint.
type ClassifyResult struct {
	Complexity Complexity `json:"complexity"`
	QALevel    QALevel    `json:"qaLevel"`
}

// QAVerdict is returned by Interpret() for the REST /interpret endpoint.
// Field names match the Mastra task-execution-workflow.ts QAVerdictSchema exactly.
type QAVerdict struct {
	TaskID     string   `json:"taskId"`
	Status     string   `json:"status"`     // pass | fail | inconclusive
	Failures   []string `json:"failures"`
	Hint       string   `json:"hint,omitempty"`
	NextAction string   `json:"nextAction"` // merge | retry | escalate | abort
}

// Router is the interface the mcp and rest packages call into.
type Router interface {
	// Route resolves a complexity to a tier, honoring force_tier if set.
	Route(complexity Complexity, forceTier Tier) (RouteResult, error)

	// RouteFallback returns the next tier in the cascade chain after fromTier.
	RouteFallback(fromTier Tier) (Tier, bool)

	// Classify maps a task description to complexity + qaLevel.
	Classify(task string) (ClassifyResult, error)

	// Interpret maps vitest output + diff + scenarios to a QA verdict.
	Interpret(vitestOutput, diff string, scenarios []string) (QAVerdict, error)
}
