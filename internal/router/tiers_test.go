package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTierConstants(t *testing.T) {
	assert.Equal(t, Tier("local_ornith"), TierLocalOrnith)
	assert.Equal(t, Tier("local_qwen"), TierLocalQwen)
	assert.Equal(t, Tier("remote_deepseek"), TierRemoteDeepSeek)
	assert.Equal(t, Tier("remote_glm"), TierRemoteGLM)
}

func TestComplexityConstants(t *testing.T) {
	assert.Equal(t, Complexity("scaffold"), ComplexityScaffold)
	assert.Equal(t, Complexity("single_file"), ComplexitySingleFile)
	assert.Equal(t, Complexity("multi_file"), ComplexityMultiFile)
	assert.Equal(t, Complexity("recovery"), ComplexityRecovery)
	assert.Equal(t, Complexity("text_op"), ComplexityTextOp)
}

func TestQALevelConstants(t *testing.T) {
	assert.Equal(t, QALevel("skip"), QALevelSkip)
	assert.Equal(t, QALevel("typecheck"), QALevelTypeCheck)
	assert.Equal(t, QALevel("full"), QALevelFull)
}

func TestRouteResultStruct(t *testing.T) {
	r := RouteResult{
		Tier:          TierLocalOrnith,
		ReasoningTags: []string{"forced_tier"},
	}
	assert.Equal(t, TierLocalOrnith, r.Tier)
	assert.Len(t, r.ReasoningTags, 1)
}

func TestClassifyResultStruct(t *testing.T) {
	c := ClassifyResult{
		Complexity: ComplexitySingleFile,
		QALevel:    QALevelFull,
	}
	assert.Equal(t, ComplexitySingleFile, c.Complexity)
	assert.Equal(t, QALevelFull, c.QALevel)
}

func TestQAVerdictStruct(t *testing.T) {
	v := QAVerdict{
		TaskID:     "1.1",
		Status:     "pass",
		Failures:   []string{},
		NextAction: "merge",
	}
	assert.Equal(t, "pass", v.Status)
	assert.Equal(t, "merge", v.NextAction)
}
