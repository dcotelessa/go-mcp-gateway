# Spec: Telemetry SDK Setup

## Purpose

Provide a single initialization entry point in `internal/telemetry` that wires
the OpenTelemetry SDK (metrics + traces), configures an OTLP/HTTP exporter, and
returns a deterministic shutdown function. Backed by Aspire Dashboard for local
development.

## Scope

- `internal/telemetry` package (NEW)
- `internal/config` telemetry section (MODIFIED)
- `cmd/gateway` bootstrap wiring (MODIFIED)
- Local dev backend: Aspire Dashboard via Docker (NEW)

## Requirements

### REQ-TSDK-01 — Initialization Function

The package SHALL expose a function `Init(ctx context.Context, cfg telemetry.Config) (Shutdown, error)`
where `Shutdown` is `func(ctx context.Context) error`.

- MUST set the global `MeterProvider` via `otel.SetMeterProvider`.
- MUST set the global `TracerProvider` via `otel.SetTracerProvider`.
- MUST set a global ` propagator ` (W3C TraceContext + Baggage) via
  `otel.SetTextMapPropagator`.
- MUST construct a `Resource` carrying `service.name` and `service.version`.

### REQ-TSDK-02 — Configuration Structure

`telemetry.Config` SHALL mirror the following YAML shape:

```yaml
telemetry:
  enabled: true
  service:
    name: "go-mcp-gateway"
    version: "0.2.0"
  otlp:
    endpoint: "localhost:4318"   # host:port, HTTP
    insecure: true
    headers: {}
  metrics:
    enabled: true
    interval: "15s"
  traces:
    enabled: true
    sampling_ratio: 1.0
```

- MUST map cleanly to `config.yaml` under the top-level `telemetry:` key.
- When `telemetry.enabled` is false, `Init` MUST return a no-op provider set
  without contacting any exporter.
- MUST default `service.name` to `"go-mcp-gateway"` when omitted.

### REQ-TSDK-03 — OTLP/HTTP Exporter

- Metrics exporter MUST be `otlpmetrichttp`.
- Trace exporter MUST be `otlptracehttp`.
- Endpoint parsing MUST accept `host:port` (no scheme) and build the full OTLP
  HTTP path automatically.
- `otlp.insecure: true` MUST disable TLS (plaintext HTTP) for local dev.

### REQ-TSDK-04 — Provider Lifecycle

- `MeterProvider` MUST use a `PeriodicReader` with the configured `metrics.interval`.
- `TracerProvider` MUST use a `BatchSpanProcessor`.
- `Shutdown(ctx)` MUST flush and shut down both providers and exporters in order:
  trace provider, then metric provider.
- `Shutdown(ctx)` MUST be idempotent — calling twice returns `nil` the second time.

### REQ-TSDK-05 — Graceful Shutdown Wiring

`cmd/gateway` MUST call the returned `Shutdown` on `SIGINT`/`SIGTERM` and during
`server.Shutdown`, draining the `ctx` timeout before process exit.

### REQ-TSDK-06 — Aspire Dashboard (Dev Backend)

- The repo MUST ship a `docker-compose.aspire.yml` (or equivalent) that starts
  Aspire Dashboard with OTLP HTTP on `localhost:4318` and UI on `localhost:18888`.
- Environment variables MUST map Aspire's OTLP listen address to the config
  default `otlp.endpoint`.

## Scenarios

### SCEN-TSDK-01 — Init with valid config registers global providers

```gherkin
GIVEN a telemetry.Config with enabled=true, otlp.endpoint="localhost:4318", insecure=true
WHEN telemetry.Init(ctx, cfg) is called
THEN otel.GetMeterProvider() returns the SDK MeterProvider
 AND otel.GetTracerProvider() returns the SDK TracerProvider
 AND no exporter dial error is returned
```
**Test:** `telemetry_test.go::TestInit_RegistersProviders`

### SCEN-TSDK-02 — Init disabled returns no-op providers

```gherkin
GIVEN a telemetry.Config with enabled=false
WHEN telemetry.Init(ctx, cfg) is called
THEN no OTLP connection is attempted
 AND otel.GetMeterProvider() is a no-op meter provider
 AND the returned Shutdown is callable and returns nil
```
**Test:** `telemetry_test.go::TestInit_Disabled_NoOp`

### SCEN-TSDK-03 — Shutdown flushes exporters and is idempotent

```gherkin
GIVEN an initialized telemetry instance wired to a fake/test exporter
WHEN Shutdown(ctx) is called once
THEN all buffered metrics and spans are flushed (exporter.Receive called)
WHEN Shutdown(ctx) is called a second time
THEN it returns nil without re-flushing
```
**Test:** `telemetry_test.go::TestShutdown_FlushesAndIsIdempotent`

### SCEN-TSDK-04 — Config deserializes telemetry section

```gherkin
GIVEN a config.yaml containing the telemetry block with endpoint, interval, sampling_ratio
WHEN config.Load("config.yaml") parses it
THEN config.Telemetry.OTLP.Endpoint == "localhost:4318"
 AND config.Telemetry.Metrics.Interval == 15*time.Second
 AND config.Telemetry.Traces.SamplingRatio == 1.0
```
**Test:** `config_test.go::TestLoad_TelemetrySection`

### SCEN-TSDK-05 — Endpoint host:port builds OTLP HTTP path

```gherkin
GIVEN otlp.endpoint="localhost:4318" and insecure=true
WHEN telemetry.Init constructs the otlpmetrichttp exporter
THEN the exporter target host is "localhost:4318"
 AND the scheme is http (not https)
 AND no TLS is configured
```
**Test:** `telemetry_test.go::TestOTLPExporter_HostPortHTTP`

### SCEN-TSDK-06 — Gateway wires Shutdown on signal

```gherkin
GIVEN the gateway process is running with telemetry.Init wired
WHEN SIGTERM is delivered to the process
THEN the telemetry Shutdown is invoked before process exit
 AND ctx passed to Shutdown honors a 5s drain timeout
```
**Test:** `main_test.go::TestSignalHandling_InvokesTelemetryShutdown`

### SCEN-TSDK-07 — Aspire compose exposes OTLP HTTP

```gherkin
GIVEN docker-compose.aspire.yml is started
WHEN telemetry.Init uses the default endpoint
THEN Aspire Dashboard receives OTLP HTTP metrics+traces on :4318
 AND the UI is reachable at http://localhost:18888
```
**Test:** `docker-compose.aspire.yml` + `make dev-telemetry` smoke check (documented)
