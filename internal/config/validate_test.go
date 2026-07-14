package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_MissingExecPath(t *testing.T) {
	cfg := Defaults()
	// ExecPath is empty by default
	err := Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exec_path")
}

func TestValidate_ExecPathNotFound(t *testing.T) {
	cfg := Defaults()
	cfg.LlamaServer.ExecPath = "/nonexistent/llama-server"
	err := Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestValidate_ModelPathNotFound(t *testing.T) {
	cfg := Defaults()

	// Create a real exec so we pass that check
	tmp := t.TempDir()
	exec := filepath.Join(tmp, "llama-server")
	require.NoError(t, os.WriteFile(exec, []byte(""), 0755))
	cfg.LlamaServer.ExecPath = exec

	cfg.Models = map[string]ModelConfig{
		"local_ornith": {
			Path:               "/nonexistent/ornith.gguf",
			VRAMRequirementMiB: 20000,
			Port:               8081,
		},
	}

	err := Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "local_ornith")
	assert.Contains(t, err.Error(), "not found")
}

func TestValidate_MissingAPIKey(t *testing.T) {
	cfg := Defaults()

	tmp := t.TempDir()
	exec := filepath.Join(tmp, "llama-server")
	require.NoError(t, os.WriteFile(exec, []byte(""), 0755))
	cfg.LlamaServer.ExecPath = exec

	// DeepSeek URL set but no key
	cfg.RemoteAPIs.DeepSeekBaseURL = "https://api.deepseek.com/v1"
	cfg.RemoteAPIs.DeepSeekAPIKey = ""

	err := Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DEEPSEEK_API_KEY")
}

func TestValidate_Valid(t *testing.T) {
	cfg := Defaults()

	tmp := t.TempDir()

	// Real exec file
	exec := filepath.Join(tmp, "llama-server")
	require.NoError(t, os.WriteFile(exec, []byte(""), 0755))
	cfg.LlamaServer.ExecPath = exec

	// Real model file
	model := filepath.Join(tmp, "ornith.gguf")
	require.NoError(t, os.WriteFile(model, []byte(""), 0644))
	cfg.Models = map[string]ModelConfig{
		"local_ornith": {Path: model, VRAMRequirementMiB: 20000, Port: 8081},
	}

	err := Validate(cfg)
	assert.NoError(t, err)
}
