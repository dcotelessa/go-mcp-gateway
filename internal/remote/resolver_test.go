package remote

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewResolver_WithKeys(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-openrouter-key")
	t.Setenv("ZAI_API_KEY", "test-zai-key")

	r, err := NewResolver()
	require.NoError(t, err)
	assert.NotNil(t, r)

	tiers := r.Available()
	assert.Contains(t, tiers, "remote_deepseek")
	assert.Contains(t, tiers, "remote_glm")
}

func TestNewResolver_NoKeys(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("ZAI_API_KEY", "")
	t.Setenv("ZHIPU_API_KEY", "")
	t.Setenv("GLM_API_KEY", "")

	r, err := NewResolver()
	require.NoError(t, err)
	assert.Empty(t, r.Available())
}

func TestResolve_DeepSeek(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("ZAI_API_KEY", "")
	t.Setenv("ZHIPU_API_KEY", "")
	t.Setenv("GLM_API_KEY", "")

	r, err := NewResolver()
	require.NoError(t, err)

	adapter, err := r.Resolve("remote_deepseek")
	require.NoError(t, err)
	assert.NotNil(t, adapter)
	assert.Contains(t, adapter.Name(), "deepseek")
}

func TestResolve_UnknownTier(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	r, err := NewResolver()
	require.NoError(t, err)

	_, err = r.Resolve("nonexistent_tier")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent_tier")
}

func TestResolve_ZhipuKeyAlias(t *testing.T) {
	// ZHIPU_API_KEY should work as alias for ZAI_API_KEY
	t.Setenv("ZAI_API_KEY", "")
	t.Setenv("GLM_API_KEY", "")
	t.Setenv("ZHIPU_API_KEY", "test-zhipu-key")
	t.Setenv("OPENROUTER_API_KEY", "test-or-key")

	r, err := NewResolver()
	require.NoError(t, err)

	adapter, err := r.Resolve("remote_glm")
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}
