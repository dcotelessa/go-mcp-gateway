# Design: v0.2 OpenTelemetry Integration

## Technical Approach

Add a single `internal/telemetry` package that owns the OTel SDK lifecycle
(provider construction, exporter wiring, instrument registration, shutdown) and
exposes narrow, allocation-cheap recording functions to the rest of the gateway.
Instrumentation of `modelmanager.Complete` and `rest /implement` is additive:
they gain a `telemetry.Handle` (or package-level accessors) but keep their
existing signatures and contracts.

Three synchronous instruments (two histograms) and one asynchronous instrument
(an observable gauge) are registered once at startup. The gauge pulls live
budget state from `internal/policy` on each collect cycle — `telemetry` never
caches or mutates budget, preserving the existing enforcement boundary.

Spans are created with a single named tracer so traces read consistently in
Aspire Dashboard: `gateway.rest.implement` (HTTP entry) parents
`modelmanager.Complete` (LLM call). Span context flows via `context.Context`,
so the completion span is a true child of the rest span without any manual
propagation code.

## Package Structure

```
internal/
  telemetry/
    telemetry.go         # Init, Shutdown, Config, Tracer(), Handle type
    instruments.go       # histogram + gauge registration, attribute keys
    record.go            # RecordTokenUsage, RecordOperationDuration wrappers
    attributes.go        # centralized semconv attribute constants (REQ-TU-01 risk note)
    budget_gauge.go      # int64 observable gauge + policy callback wiring
    telemetry_test.go
    budget_gauge_test.go
  config/
    config.go            # + Telemetry struct fields (MODIFIED)
    config_test.go
  modelmanager/
    complete.go          # + spans + metrics emission (MODIFIED)
    complete_metrics_test.go
    complete_trace_test.go
  rest/
    implement.go         # + gateway.rest.implement span (MODIFIED)
    implement_trace_test.go
    implement_contract_test.go
  policy/
    budget.go            # + RemainingByTier() accessor used by the gauge (read-only)
cmd/gateway/
    main.go              # wire telemetry.Init + Shutdown on signal (MODIFIED)
docker-compose.aspire.yml        # Aspire Dashboard dev backend (NEW)
Makefile                        # dev-telemetry target (NEW)
```

## Libraries and Versions

All from the stable OTel Go SDK v1.x line. Pin to a recent stable release.

| Module | Version | Purpose |
|---|---|---|
| `go.opentelemetry.io/otel` | v1.28.0 | core API, global providers |
| `go.opentelemetry.io/otel/sdk` | v1.28.0 | MeterProvider, TracerProvider, Resource, BatchSpanProcessor |
| `go.opentelemetry.io/otel/sdk/metric` | v1.28.0 | PeriodicReader, instrument registration |
| `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp` | v1.28.0 | metrics OTLP/HTTP exporter |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` | v1.28.0 | traces OTLP/HTTP exporter |
| `go.opentelemetry.io/otel/trace` | v1.28.0 | tracer, span, status APIs |
| `go.opentelemetry.io/otel/propagation` | v1.28.0 | W3C TraceContext + Baggage propagator |
| `go.opentelemetry.io/otel/sdk/resource` | v1.28.0 | Resource with service.name/version |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | v0.53.0 | (optional) HTTP middleware for rest |

> The proposal mandates referencing the **stable metrics API** (SDK v1.x). The
> metrics SDK graduated to stable in v1.17.0+; v1.28.0 is chosen as a current
> stable point. The trace SDK is also stable.

### Dev Backend

- **Aspire Dashboard** image: `mcr.microsoft.com/dotnet/nightly/aspire-dashboard:latest`
- OTLP HTTP listener: `:4318` (metrics + traces over HTTP)
- Dashboard UI: `:18888`
- Configured via `docker-compose.aspire.yml`; env `ASPIRE_DASHBOARD_OTLP_ENDPOINT_URL=http://0.0.0.0:18888`

## Architectural Decisions

References to the proposal's stated decisions and risks:

- **D1 — Single OTLP/HTTP exporter (not gRPC).** Reduces operational surface
  for local dev; Aspire Dashboard speaks HTTP OTLP natively. Maps to focus
  area #5 (OTLP HTTP exporter). Risk: LOW.
- **D2 — Observable gauge pulls from `internal/policy` (read-only).** Avoids
  duplicating budget state and keeps enforcement single-source. The gauge
  callback calls a new read-only `policy.RemainingByTier()` accessor. Maps to
  REQ-BG-02 / REQ-BG-04.
- **D3 — Synchronous histograms for token usage + duration.** Push on
  completion (low rate: one record per LLM call). The duration histogram is
  recorded on both success and failure; token usage only on success. Maps to
  REQ-TU-03 / REQ-OD-03.
- **D4 — Centralized attribute constants.** Addresses the **MEDIUM risk** that
  GenAI semconv attribute names are still Development status. All five
  attribute keys live in `telemetry/attributes.go`; renaming is a one-file
  change. Custom gateway attributes (`gateway.tier`, `gateway.complexity`) are
  namespaced under `gateway.` to avoid collision with future semconv.
- **D5 — No-op fast path when disabled.** When `telemetry.enabled=false`,
  `Init` installs SDK no-op providers so recording functions compile to cheap
  calls; no exporter is dialed. Maps to REQ-TU-02 / REQ-OD-02 / REQ-TR-04.
- **D6 — Aspire for dev, Grafana for prod (two configs).** Accepted risk
  (LOW). Only the `otlp.endpoint` differs between environments; the SDK code is
  identical. Aspire compose lives in-repo; prod Grafana config is documented
  but out of scope for v0.2.
- **D7 — Span duration reuses the duration-histogram timing.** A single
  `start := time.Now()` is captured at the top of `modelmanager.Complete`; the
  span ends and `RecordOperationDuration` records `time.Since(start)`. Avoids
  double instrumentation and timing skew. Maps to REQ-OD-03 / REQ-TR-03.
- **D8 — Contract preservation.** Instrumentation is purely additive;
  `/implement` status codes, headers, body, and reasoning tags are unchanged.
  Enforced by SCEN-TR-07 (byte-identical baseline replay). Maps to the
  proposal's "What Won't Change" list.

## Data Flow

```
HTTP POST /implement
   │  rest: span "gateway.rest.implement" started, http.* attrs set
   ▼
router selects tier + model  ──►  span.SetAttributes(gateway.tier, gateway.complexity, gen_ai.request.model)
   │
   ▼
modelmanager.Complete(ctx, …)
   │  child span "modelmanager.Complete" started (shares trace)
   │  start := time.Now()
   ▼
upstream provider call (local/remote)
   │
   ▼  on return:
   │   ├─ RecordOperationDuration(time.Since(start))   [success + failure]
   │   ├─ RecordTokenUsage(input)  RecordTokenUsage(output) [success only]
   │   └─ span events llm.completion.start / llm.completion.end
   ▼
policy.RemainingByTier() updated  ──►  collected by PeriodicReader
   │                                    └─► gateway.tier.budget.remaining gauge
   ▼
OTLP/HTTP :4318 ──► Aspire Dashboard (dev) / Grafana (prod)
```

## Configuration Reference

`config.yaml`:

```yaml
telemetry:
  enabled: true
  service:
    name: "go-mcp-gateway"
    version: "0.2.0"
  otlp:
    endpoint: "localhost:4318"
    insecure: true
    headers: {}
  metrics:
    enabled: true
    interval: "15s"
  traces:
    enabled: true
    sampling_ratio: 1.0
```

## Testing Strategy

- **In-memory metric reader** (`metric.NewManualReader`) for histogram/gauge
  assertions without a live exporter.
- **In-memory span exporter** (`tracetest.NewInMemoryExporter`) for span
  parent/child, attribute, and event assertions.
- **Fake provider** in `modelmanager` tests returning deterministic usage +
  configurable latency/error.
- **Contract baseline test** captures the uninstrumented `/implement` response
  bytes and replays after instrumentation for byte-equality.
- All scenarios in `specs/*.md` map 1:1 to the Go test stubs in `tasks.md`.
