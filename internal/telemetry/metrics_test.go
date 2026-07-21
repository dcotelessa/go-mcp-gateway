package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func setupTestMeter(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
	)
	otel.SetMeterProvider(mp)
	t.Cleanup(func() {
		_ = mp.Shutdown(context.Background())
		metricsInitialized = false
		tokenUsageHistogram = nil
		opDurationHistogram = nil
	})
	return reader
}

func TestTokenUsageHistogram_Registration(t *testing.T) {
	reader := setupTestMeter(t)
	require.NoError(t, InitMetrics())

	// Record a value
	RecordTokenUsage(context.Background(), TokenUsageAttrs{
		System:     "llama_cpp",
		Model:      "ornith-35b",
		Tier:       "local_ornith",
		Complexity: "scaffold",
	}, TokenTypeInput, 100)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "gen_ai.client.token.usage" {
				found = true
				assert.Equal(t, "{token}", m.Unit)
			}
		}
	}
	assert.True(t, found, "gen_ai.client.token.usage metric must be registered")
}

func TestDurationHistogram_Registration(t *testing.T) {
	reader := setupTestMeter(t)
	require.NoError(t, InitMetrics())

	RecordOperationDuration(context.Background(), OpAttrs{
		System:     "llama_cpp",
		Model:      "ornith-35b",
		Tier:       "local_ornith",
		Complexity: "scaffold",
		Operation:  "chat",
	}, 1.5)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "gen_ai.client.operation.duration" {
				found = true
				assert.Equal(t, "s", m.Unit)
			}
		}
	}
	assert.True(t, found, "gen_ai.client.operation.duration metric must be registered")
}

func TestRecordTokenUsage_UnknownAttrFallback(t *testing.T) {
	setupTestMeter(t)
	require.NoError(t, InitMetrics())

	// Should not panic with empty attrs
	assert.NotPanics(t, func() {
		RecordTokenUsage(context.Background(), TokenUsageAttrs{}, "", 50)
	})
}

func TestRecordTokenUsage_NoOp_WhenNotInitialized(t *testing.T) {
	// Without InitMetrics, recording should be a no-op
	metricsInitialized = false
	tokenUsageHistogram = nil

	assert.NotPanics(t, func() {
		RecordTokenUsage(context.Background(), TokenUsageAttrs{
			System: "llama_cpp",
			Model:  "ornith-35b",
		}, TokenTypeInput, 100)
	})
}

func TestBudgetGauge_Registration(t *testing.T) {
	reader := setupTestMeter(t)

	err := RegisterBudgetGauge(func() map[string]int64 {
		return map[string]int64{
			"remote_deepseek": 500000,
			"remote_glm":      250000,
			"local_ornith":    -1, // slot-based, should be omitted
		}
	})
	require.NoError(t, err)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "gateway.tier.budget.remaining" {
				found = true
				assert.Equal(t, "{token}", m.Unit)
			}
		}
	}
	assert.True(t, found, "gateway.tier.budget.remaining gauge must be registered")
}

func TestBudgetGauge_LocalTierOmitted(t *testing.T) {
	reader := setupTestMeter(t)

	err := RegisterBudgetGauge(func() map[string]int64 {
		return map[string]int64{
			"local_ornith": -1, // slot-based, remaining < 0 → omit
		}
	})
	require.NoError(t, err)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	// Should have no data points for local tiers
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "gateway.tier.budget.remaining" {
				g, ok := m.Data.(metricdata.Gauge[int64])
				if ok {
					assert.Empty(t, g.DataPoints,
						"local tiers with remaining=-1 must not be observed")
				}
			}
		}
	}
}
