package modelmanager

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfig() ManagerConfig {
	return ManagerConfig{
		ExecPath:         "/bin/echo", // safe stand-in for tests
		HealthTimeoutSec: 5,
		StopTimeoutSec:   2,
		LogDir:           "/tmp",
		TotalVRAMMiB:     16311,
		ReservedVRAMMiB:  1024,
		Models: map[string]ModelConfig{
			"local_ornith": {
				Path:               "/tmp/ornith.gguf",
				VRAMRequirementMiB: 20000,
				Port:               8081,
			},
			"local_qwen": {
				Path:               "/tmp/qwen.gguf",
				VRAMRequirementMiB: 22000,
				Port:               8082,
			},
		},
	}
}

func TestResidentState_Fields(t *testing.T) {
	s := ResidentState{
		Tier:      "local_ornith",
		ModelPath: "/tmp/ornith.gguf",
		Port:      8081,
		PID:       12345,
		StartedAt: time.Now(),
		APIKey:    "abc123",
	}
	assert.Equal(t, "local_ornith", s.Tier)
	assert.Equal(t, 8081, s.Port)
	assert.Equal(t, 12345, s.PID)
	assert.False(t, s.Swapping)
}

func TestManager_New(t *testing.T) {
	m := New(testConfig())
	require.NotNil(t, m)
	assert.Nil(t, m.Resident(), "no model resident at startup")
	m.done <- struct{}{} // stop swap processor
}

func TestManager_Resident_NilInitially(t *testing.T) {
	m := New(testConfig())
	assert.Nil(t, m.Resident())
	m.done <- struct{}{}
}

func TestGenerateAPIKey(t *testing.T) {
	key1, err := generateAPIKey()
	require.NoError(t, err)
	assert.Len(t, key1, 64, "32 bytes = 64 hex chars")

	key2, err := generateAPIKey()
	require.NoError(t, err)
	assert.NotEqual(t, key1, key2, "keys must be unique per generation")
}

func TestBuildArgs(t *testing.T) {
	model := ModelConfig{
		Path:      "/tmp/ornith.gguf",
		Port:      8081,
		ExtraArgs: []string{"--ctx-size", "4096"},
	}
	args := buildArgs(model, "test-api-key")

	assert.Contains(t, args, "--model")
	assert.Contains(t, args, "/tmp/ornith.gguf")
	assert.Contains(t, args, "--port")
	assert.Contains(t, args, "8081")
	assert.Contains(t, args, "--api-key")
	assert.Contains(t, args, "test-api-key")
	assert.Contains(t, args, "--ctx-size")
	assert.Contains(t, args, "4096")
}
