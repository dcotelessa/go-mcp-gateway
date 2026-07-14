package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoute_ComplexityMapping(t *testing.T) {
	r := New()

	cases := []struct {
		complexity Complexity
		wantTier   Tier
	}{
		{ComplexityScaffold, TierLocalOrnith},
		{ComplexitySingleFile, TierLocalOrnith},
		{ComplexityRecovery, TierLocalOrnith},
		{ComplexityTextOp, TierLocalQwen},
		{ComplexityMultiFile, TierRemoteDeepSeek},
	}

	for _, tc := range cases {
		result, err := r.Route(tc.complexity, "")
		require.NoError(t, err)
		assert.Equal(t, tc.wantTier, result.Tier)
		assert.Empty(t, result.ReasoningTags)
	}
}

func TestRoute_ForceTier(t *testing.T) {
	r := New()

	result, err := r.Route(ComplexitySingleFile, TierRemoteGLM)
	require.NoError(t, err)
	assert.Equal(t, TierRemoteGLM, result.Tier)
	assert.Contains(t, result.ReasoningTags, "forced_tier")
}

func TestRoute_InvalidComplexity(t *testing.T) {
	r := New()

	_, err := r.Route("invalid_complexity", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown complexity")
}

func TestRouteFallback_Chain(t *testing.T) {
	r := New()

	next, ok := r.RouteFallback(TierRemoteGLM)
	assert.True(t, ok)
	assert.Equal(t, TierRemoteDeepSeek, next)

	next, ok = r.RouteFallback(TierRemoteDeepSeek)
	assert.True(t, ok)
	assert.Equal(t, TierLocalOrnith, next)

	_, ok = r.RouteFallback(TierLocalOrnith)
	assert.False(t, ok, "local_ornith is terminal — no further fallback")

	_, ok = r.RouteFallback(TierLocalQwen)
	assert.False(t, ok, "local_qwen is terminal — no further fallback")
}

func TestClassify(t *testing.T) {
	r := New()

	cases := []struct {
		task           string
		wantComplexity Complexity
		wantQALevel    QALevel
	}{
		{"rename the function handleRequest to processRequest", ComplexityTextOp, QALevelSkip},
		{"scaffold the config package with default structs", ComplexityScaffold, QALevelSkip},
		{"fix the failing test in router_test.go", ComplexityRecovery, QALevelFull},
		{"coordinate changes across multiple files in the lsp package", ComplexityMultiFile, QALevelFull},
		{"implement the health check polling function", ComplexitySingleFile, QALevelFull},
	}

	for _, tc := range cases {
		result, err := r.Classify(tc.task)
		require.NoError(t, err)
		assert.Equal(t, tc.wantComplexity, result.Complexity, "task: %s", tc.task)
		assert.Equal(t, tc.wantQALevel, result.QALevel, "task: %s", tc.task)
	}
}

func TestClassify_EmptyTask(t *testing.T) {
	r := New()
	_, err := r.Classify("")
	assert.Error(t, err)
}

func TestInterpret_Pass(t *testing.T) {
	r := New()
	verdict, err := r.Interpret("All tests passed (5/5)", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "pass", verdict.Status)
	assert.Equal(t, "merge", verdict.NextAction)
}

func TestInterpret_Fail(t *testing.T) {
	r := New()
	output := "× router_test.go:42 expected ornith got qwen\nfailed 1 test"
	verdict, err := r.Interpret(output, "", nil)
	require.NoError(t, err)
	assert.Equal(t, "fail", verdict.Status)
	assert.Equal(t, "retry", verdict.NextAction)
	assert.NotEmpty(t, verdict.Failures)
}

func TestInterpret_Inconclusive(t *testing.T) {
	r := New()
	verdict, err := r.Interpret("no output", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "inconclusive", verdict.Status)
	assert.Equal(t, "escalate", verdict.NextAction)
}
