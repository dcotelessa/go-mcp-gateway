package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// TokenUsageAttrs holds the attributes for a token usage recording.
type TokenUsageAttrs struct {
	System     string // gen_ai.system
	Model      string // gen_ai.request.model
	Tier       string // gateway.tier
	Complexity string // gateway.complexity
}

// OpAttrs holds the attributes for an operation duration recording.
type OpAttrs struct {
	System     string // gen_ai.system
	Model      string // gen_ai.request.model
	Tier       string // gateway.tier
	Complexity string // gateway.complexity
	Operation  string // gen_ai.operation.name
}

var (
	tokenUsageHistogram    metric.Int64Histogram
	opDurationHistogram    metric.Float64Histogram
	metricsInitialized     bool
)

// InitMetrics registers all gateway metric instruments.
// Must be called after Init() sets the global MeterProvider.
func InitMetrics() error {
	meter := otel.Meter(instrumentationName)

	var err error

	// gen_ai.client.token.usage — histogram of token counts
	// Unit: {token} per OTel semantic conventions
	tokenUsageHistogram, err = meter.Int64Histogram(
		"gen_ai.client.token.usage",
		metric.WithDescription("Number of tokens used per LLM request"),
		metric.WithUnit("{token}"),
		metric.WithExplicitBucketBoundaries(
			1, 10, 50, 100, 500, 1000, 2000, 5000, 10000, 50000,
		),
	)
	if err != nil {
		return err
	}

	// gen_ai.client.operation.duration — histogram of latency in seconds
	opDurationHistogram, err = meter.Float64Histogram(
		"gen_ai.client.operation.duration",
		metric.WithDescription("Duration of GenAI completion operations"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(
			0.1, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0, 60.0, 120.0,
		),
	)
	if err != nil {
		return err
	}

	metricsInitialized = true
	return nil
}

// RecordTokenUsage records input or output token counts for a completion.
// Falls back to "unknown" for any missing attribute value.
func RecordTokenUsage(ctx context.Context, attrs TokenUsageAttrs, tokenType string, tokens int64) {
	if !metricsInitialized || tokenUsageHistogram == nil {
		return
	}

	system := attrs.System
	if system == "" {
		system = "unknown"
	}
	model := attrs.Model
	if model == "" {
		model = "unknown"
	}
	tier := attrs.Tier
	if tier == "" {
		tier = "unknown"
	}
	complexity := attrs.Complexity
	if complexity == "" {
		complexity = "unknown"
	}
	if tokenType == "" {
		tokenType = "unknown"
	}

	tokenUsageHistogram.Record(ctx, tokens,
		metric.WithAttributes(
			AttrGenAISystem.String(system),
			AttrGenAIRequestModel.String(model),
			AttrGenAITokenType.String(tokenType),
			AttrGatewayTier.String(tier),
			AttrGatewayComplexity.String(complexity),
		),
	)
}

// RecordOperationDuration records the latency of a completion in seconds.
func RecordOperationDuration(ctx context.Context, attrs OpAttrs, seconds float64) {
	if !metricsInitialized || opDurationHistogram == nil {
		return
	}

	system := attrs.System
	if system == "" {
		system = "unknown"
	}
	op := attrs.Operation
	if op == "" {
		op = "chat"
	}

	opDurationHistogram.Record(ctx, seconds,
		metric.WithAttributes(
			AttrGenAISystem.String(system),
			AttrGenAIRequestModel.String(attrs.Model),
			AttrGenAIOperationName.String(op),
			AttrGatewayTier.String(attrs.Tier),
			AttrGatewayComplexity.String(attrs.Complexity),
		),
	)
}

// BudgetGaugeFunc is a callback type for the budget gauge observable.
type BudgetGaugeFunc func() map[string]int64

// RegisterBudgetGauge registers gateway.tier.budget.remaining as an
// observable gauge backed by the provided callback.
func RegisterBudgetGauge(remainingFn BudgetGaugeFunc) error {
	meter := otel.Meter(instrumentationName)

	_, err := meter.Int64ObservableGauge(
		"gateway.tier.budget.remaining",
		metric.WithDescription("Remaining token budget per remote tier"),
		metric.WithUnit("{token}"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			for tier, remaining := range remainingFn() {
				if remaining < 0 {
					continue // skip local tiers (slot-based, not token-based)
				}
				o.Observe(remaining,
					metric.WithAttributes(
						AttrGatewayTier.String(tier),
					),
				)
			}
			return nil
		}),
	)
	return err
}

// attrs helper — builds a standard attribute set from TokenUsageAttrs.
func tokenAttrs(a TokenUsageAttrs, tokenType string) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrGenAISystem.String(a.System),
		AttrGenAIRequestModel.String(a.Model),
		AttrGenAITokenType.String(tokenType),
		AttrGatewayTier.String(a.Tier),
		AttrGatewayComplexity.String(a.Complexity),
	}
}

var _ = tokenAttrs // suppress unused warning until used in traces
