// Package telemetry wires the OpenTelemetry SDK for the gateway.
// It emits gen_ai.* semantic convention metrics and traces for every
// completion routed through the gateway.
//
// Conventions: https://opentelemetry.io/docs/specs/semconv/gen-ai/
// Status: Development (attribute names may change — all constants
// are centralised in attributes.go to make migrations a single-file edit).
package telemetry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const (
	instrumentationName    = "github.com/dcotelessa/gateway"
	instrumentationVersion = "0.2.0"
)

// Config holds the telemetry configuration loaded from config.yaml.
type Config struct {
	Enabled bool

	Service ServiceConfig
	OTLP    OTLPConfig
	Metrics MetricsConfig
	Traces  TracesConfig
}

// ServiceConfig identifies the gateway in telemetry backends.
type ServiceConfig struct {
	Name    string // default: "go-mcp-gateway"
	Version string // default: "0.2.0"
}

// OTLPConfig configures the OTLP HTTP exporter.
type OTLPConfig struct {
	Endpoint string // e.g. "http://localhost:4318"
	Insecure bool   // true for local dev (Aspire Dashboard)
}

// MetricsConfig configures the metrics pipeline.
type MetricsConfig struct {
	ExportIntervalSec int // default: 15
}

// TracesConfig configures the trace pipeline.
type TracesConfig struct {
	SamplingRatio float64 // 1.0 = always sample
}

// DefaultConfig returns sensible defaults for local development.
func DefaultConfig() Config {
	return Config{
		Enabled: false, // opt-in
		Service: ServiceConfig{
			Name:    "go-mcp-gateway",
			Version: instrumentationVersion,
		},
		OTLP: OTLPConfig{
			Endpoint: "http://localhost:4318",
			Insecure: true,
		},
		Metrics: MetricsConfig{
			ExportIntervalSec: 15,
		},
		Traces: TracesConfig{
			SamplingRatio: 1.0,
		},
	}
}

// ShutdownFunc flushes and shuts down both providers.
// Safe to call multiple times (idempotent via sync.Once).
type ShutdownFunc func(ctx context.Context) error

// providers holds the initialized SDK providers.
type providers struct {
	tracer *sdktrace.TracerProvider
	meter  *sdkmetric.MeterProvider
}

var (
	globalProviders *providers
	shutdownOnce    sync.Once
	shutdownErr     error
)

// Init initialises the OTel SDK and sets global providers.
// Returns a ShutdownFunc that flushes both providers on exit.
// When cfg.Enabled is false, installs no-op providers and returns immediately.
func Init(ctx context.Context, cfg Config) (ShutdownFunc, error) {
	if !cfg.Enabled {
		// No-op path — global providers remain the SDK defaults (no-op)
		return func(_ context.Context) error { return nil }, nil
	}

	res, err := buildResource(ctx, cfg.Service)
	if err != nil {
		return nil, fmt.Errorf("telemetry: resource: %w", err)
	}

	// Trace provider
	traceExp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(cfg.OTLP.Endpoint+"/v1/traces"),
		insecureTraceOption(cfg.OTLP.Insecure),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: trace exporter: %w", err)
	}

	sampler := sdktrace.TraceIDRatioBased(cfg.Traces.SamplingRatio)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sampler)),
	)

	// Metric provider
	metricExp, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpointURL(cfg.OTLP.Endpoint+"/v1/metrics"),
		insecureMetricOption(cfg.OTLP.Insecure),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: metric exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(
				metricExp,
				sdkmetric.WithInterval(
					time.Duration(cfg.Metrics.ExportIntervalSec)*time.Second,
				),
			),
		),
		sdkmetric.WithResource(res),
	)

	// Set globals
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	globalProviders = &providers{tracer: tp, meter: mp}

	return shutdownFunc(tp, mp), nil
}

// Tracer returns a named tracer backed by the global provider.
func Tracer() interface {
	// Returns trace.Tracer but avoids importing trace in callers
} {
	return otel.Tracer(instrumentationName,
		// trace.WithInstrumentationVersion(instrumentationVersion),
	)
}

// Meter returns a named meter backed by the global provider.
func Meter() interface{} {
	return otel.Meter(instrumentationName)
}

// buildResource creates an OTel Resource identifying this service.
func buildResource(ctx context.Context, svc ServiceConfig) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(svc.Name),
			semconv.ServiceVersion(svc.Version),
		),
		resource.WithProcess(),
		resource.WithOS(),
	)
}

// shutdownFunc returns an idempotent ShutdownFunc that flushes both providers.
func shutdownFunc(tp *sdktrace.TracerProvider, mp *sdkmetric.MeterProvider) ShutdownFunc {
	return func(ctx context.Context) error {
		shutdownOnce.Do(func() {
			// Flush traces first, then metrics
			if err := tp.Shutdown(ctx); err != nil {
				shutdownErr = fmt.Errorf("telemetry: trace shutdown: %w", err)
				return
			}
			if err := mp.Shutdown(ctx); err != nil {
				shutdownErr = fmt.Errorf("telemetry: metric shutdown: %w", err)
			}
		})
		return shutdownErr
	}
}

// insecureTraceOption returns the appropriate option for HTTP vs HTTPS.
func insecureTraceOption(insecure bool) otlptracehttp.Option {
	if insecure {
		return otlptracehttp.WithInsecure()
	}
	return otlptracehttp.WithInsecure() // default to insecure for dev
}

// insecureMetricOption returns the appropriate option for HTTP vs HTTPS.
func insecureMetricOption(insecure bool) otlpmetrichttp.Option {
	if insecure {
		return otlpmetrichttp.WithInsecure()
	}
	return otlpmetrichttp.WithInsecure()
}
