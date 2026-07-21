package telemetry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSystemForTier_KnownTiers(t *testing.T) {
	assert.Equal(t, "llama_cpp", SystemForTier("local_ornith"))
	assert.Equal(t, "llama_cpp", SystemForTier("local_qwen"))
	assert.Equal(t, "llama_cpp", SystemForTier("local_glm"))
	assert.Equal(t, "deepseek", SystemForTier("remote_deepseek"))
	assert.Equal(t, "z_ai", SystemForTier("remote_glm"))
}

func TestSystemForTier_UnknownFallback(t *testing.T) {
	assert.Equal(t, "unknown", SystemForTier("unknown_tier"))
	assert.Equal(t, "unknown", SystemForTier(""))
}

func TestAttrKeys_NotEmpty(t *testing.T) {
	assert.NotEmpty(t, string(AttrGenAISystem))
	assert.NotEmpty(t, string(AttrGenAIRequestModel))
	assert.NotEmpty(t, string(AttrGenAITokenType))
	assert.NotEmpty(t, string(AttrGatewayTier))
	assert.NotEmpty(t, string(AttrGatewayComplexity))
}

func TestTokenTypeConstants(t *testing.T) {
	assert.Equal(t, "input", TokenTypeInput)
	assert.Equal(t, "output", TokenTypeOutput)
}
