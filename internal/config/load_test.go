package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults_NoFile(t *testing.T) {
	t.Setenv("GATEWAY_CONFIG", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("GLM_API_KEY", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, 20, cfg.Policy.SessionRatePerMin)
}

func TestLoadFromYAML(t *testing.T) {
	yaml := `
server:
  port: 9090
policy:
  session_rate_per_min: 50
models:
  local_ornith:
    path: /home/dcotelessa/models/ornith-1.0-35b-Q4_K_M.gguf
    vram_requirement_mib: 20000
    port: 8081
`
	tmp := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(tmp, []byte(yaml), 0644))
	t.Setenv("GATEWAY_CONFIG", tmp)

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 9090, cfg.Server.Port)
	assert.Equal(t, 50, cfg.Policy.SessionRatePerMin)
	ornith, ok := cfg.Models["local_ornith"]
	require.True(t, ok, "local_ornith should be in model registry")
	assert.Equal(t, "/home/dcotelessa/models/ornith-1.0-35b-Q4_K_M.gguf", ornith.Path)
	assert.Equal(t, 20000, ornith.VRAMRequirementMiB)
	assert.Equal(t, 8081, ornith.Port)
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("GATEWAY_CONFIG", "")
	t.Setenv("DEEPSEEK_API_KEY", "ds-test-key")
	t.Setenv("GLM_API_KEY", "glm-test-key")
	t.Setenv("LLAMA_SERVER_PATH", "/usr/local/bin/llama-server")
	t.Setenv("SESSION_RATE_PER_MIN", "42")
	t.Setenv("SESSION_TOKENS_PER_HOUR", "99999")
	t.Setenv("BUDGET_DEEPSEEK_DAILY", "500000")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "ds-test-key", cfg.RemoteAPIs.DeepSeekAPIKey)
	assert.Equal(t, "glm-test-key", cfg.RemoteAPIs.GLMAPIKey)
	assert.Equal(t, "/usr/local/bin/llama-server", cfg.LlamaServer.ExecPath)
	assert.Equal(t, 42, cfg.Policy.SessionRatePerMin)
	assert.Equal(t, 99999, cfg.Policy.SessionTokensPerHour)
	assert.Equal(t, 500000, cfg.Policy.BudgetDeepSeekDaily)
}

func TestLoadInvalidYAML(t *testing.T) {
	// A mapping key with a tab-indented value trips the YAML parser
	// when unmarshalling into a typed struct
	invalid := `
server:
	port: [unclosed
`
	tmp := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(tmp, []byte(invalid), 0644))
	t.Setenv("GATEWAY_CONFIG", tmp)

	_, err := Load()
	assert.Error(t, err, "malformed YAML should return an error")
}

func TestLoadTelemetrySection(t *testing.T) {
	yaml := `
telemetry:
  enabled: true
  service:
    name: my-gateway
    version: "0.2.0"
  otlp:
    endpoint: http://otel-collector:4318
    insecure: true
  metrics:
    export_interval_sec: 30
  traces:
    sampling_ratio: 0.5
`
	tmp := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(tmp, []byte(yaml), 0644))
	t.Setenv("GATEWAY_CONFIG", tmp)

	cfg, err := Load()
	require.NoError(t, err)

	assert.True(t, cfg.Telemetry.Enabled)
	assert.Equal(t, "my-gateway", cfg.Telemetry.Service.Name)
	assert.Equal(t, "http://otel-collector:4318", cfg.Telemetry.OTLP.Endpoint)
	assert.Equal(t, 30, cfg.Telemetry.Metrics.ExportIntervalSec)
	assert.Equal(t, 0.5, cfg.Telemetry.Traces.SamplingRatio)
}
