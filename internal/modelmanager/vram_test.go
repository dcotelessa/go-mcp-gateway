package modelmanager

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeBudget_FitsInVRAM(t *testing.T) {
	// GLM-4.7 at 13000 MiB fits within available VRAM (15287 MiB) — no split
	budget, err := ComputeBudget(16311, 1024, 13000)
	require.NoError(t, err)
	assert.False(t, budget.NeedsLayerSplit)
	assert.Equal(t, 15287, budget.AvailableMiB)
}

func TestComputeBudget_NeedsLayerSplit(t *testing.T) {
	// Ornith 35B at 20000 MiB exceeds available VRAM (15287 MiB)
	// but fits within hard ceiling — llama.cpp offloads layers to RAM
	budget, err := ComputeBudget(16311, 1024, 20000)
	require.NoError(t, err)
	assert.True(t, budget.NeedsLayerSplit,
		"Ornith 35B exceeds available VRAM but llama.cpp handles via -sm layer")
}

func TestComputeBudget_QwenNeedsLayerSplit(t *testing.T) {
	// Qwen 3.6 35B at 22000 MiB — same pattern as Ornith
	budget, err := ComputeBudget(16311, 1024, 22000)
	require.NoError(t, err)
	assert.True(t, budget.NeedsLayerSplit)
}

func TestComputeBudget_TooLarge(t *testing.T) {
	// Absurdly large model exceeds hard ceiling (16311 * 4 = 65244 MiB)
	_, err := ComputeBudget(16311, 1024, 100000)
	assert.ErrorIs(t, err, ErrModelTooLargeForVRAM)
}

func TestComputeBudget_ExactCeiling(t *testing.T) {
	// Model at exactly the hard ceiling should be rejected
	ceiling := 16311 * hardCeilingMultiplier
	_, err := ComputeBudget(16311, 1024, ceiling+1)
	assert.ErrorIs(t, err, ErrModelTooLargeForVRAM)
}

func TestLayerSplitArgs_WhenNeeded(t *testing.T) {
	budget := VRAMBudget{NeedsLayerSplit: true}
	args := LayerSplitArgs(budget)
	assert.Equal(t, []string{"-sm", "layer"}, args)
}

func TestLayerSplitArgs_WhenNotNeeded(t *testing.T) {
	budget := VRAMBudget{NeedsLayerSplit: false}
	args := LayerSplitArgs(budget)
	assert.Nil(t, args)
}
