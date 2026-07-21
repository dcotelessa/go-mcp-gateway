package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()

	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "/mcp", cfg.MCP.EndpointPath)
	assert.Equal(t, 30, cfg.MCP.SessionIdleTTLMinutes)
	assert.Equal(t, 16311, cfg.VRAM.TotalMiB)
	assert.Equal(t, 1024, cfg.VRAM.ReservedMiB)
	assert.Equal(t, 20, cfg.Policy.SessionRatePerMin)
	assert.Equal(t, 200000, cfg.Policy.SessionTokensPerHour)
	assert.Equal(t, 15, cfg.LSP.IdleTimeoutMin)
	assert.Equal(t, 10, cfg.REST.ShutdownDrainSec)
}

func TestConfigStructFields(t *testing.T) {
	// Verify all structs initialize to zero value without panic
	var cfg Config
	assert.Empty(t, cfg.LlamaServer.ExecPath)
	assert.Nil(t, cfg.Models)
	assert.Empty(t, cfg.RemoteAPIs.DeepSeekAPIKey)
}

func TestDefaults_TelemetrySection(t *testing.T) {
	cfg := Defaults()
	assert.False(t, cfg.Telemetry.Enabled)
	assert.Equal(t, "go-mcp-gateway", cfg.Telemetry.Service.Name)
	assert.Equal(t, "http://localhost:4318", cfg.Telemetry.OTLP.Endpoint)
	assert.True(t, cfg.Telemetry.OTLP.Insecure)
	assert.Equal(t, 15, cfg.Telemetry.Metrics.ExportIntervalSec)
	assert.Equal(t, 1.0, cfg.Telemetry.Traces.SamplingRatio)
}
