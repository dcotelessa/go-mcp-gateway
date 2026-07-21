package telemetry

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.False(t, cfg.Enabled, "telemetry should be opt-in by default")
	assert.Equal(t, "go-mcp-gateway", cfg.Service.Name)
	assert.Equal(t, "http://localhost:4318", cfg.OTLP.Endpoint)
	assert.True(t, cfg.OTLP.Insecure)
	assert.Equal(t, 15, cfg.Metrics.ExportIntervalSec)
	assert.Equal(t, 1.0, cfg.Traces.SamplingRatio)
}

func TestInit_Disabled_NoOp(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false

	shutdown, err := Init(context.Background(), cfg)
	require.NoError(t, err)
	assert.NotNil(t, shutdown)
	assert.NoError(t, shutdown(context.Background()))
}

func TestInit_GlobalsAccessible_AfterDisabled(t *testing.T) {
	cfg := DefaultConfig()
	_, err := Init(context.Background(), cfg)
	require.NoError(t, err)

	assert.NotNil(t, otel.Tracer("test"))
	assert.NotNil(t, otel.Meter("test"))
}

func TestShutdown_Idempotent(t *testing.T) {
	// Reset global shutdown state
	shutdownOnce = sync.Once{}
	shutdownErr = nil

	tp := sdktrace.NewTracerProvider()
	mp := sdkmetric.NewMeterProvider()
	fn := shutdownFunc(tp, mp)

	assert.NoError(t, fn(context.Background()))
	assert.NoError(t, fn(context.Background()))
}
