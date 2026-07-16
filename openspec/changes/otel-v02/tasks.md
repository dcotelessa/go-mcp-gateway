# Tasks: v0.2 OpenTelemetry Integration

Tasks are grouped by spec area. Each task is independently reviewable and
completable in one agent session. Test stubs map 1:1 to spec scenarios.

---

## 1. Telemetry SDK Foundation

- [ ] 1.1 Create `internal/telemetry` package skeleton: `telemetry.go` with `Config` struct mirroring `config.yaml` telemetry section (REQ-TSDK-02). `complexity: scaffold`
- [ ] 1.2 Add OTel SDK + OTLP/HTTP exporter deps to `go.mod`: `go.opentelemetry.io/otel@v1.28.0`, `/sdk`, `/sdk/metric`, `/sdk/trace`, `/sdk/resource`, `/propagation`, `otlpmetrichttp`, `otlptracehttp`. `complexity: single_file`
- [ ] 1.3 Implement `telemetry.Init(ctx, cfg) (Shutdown, error)`: builds Resource (service.name+version), MeterProvider with PeriodicReader, TracerProvider with BatchSpanProcessor, sets globals + W3C propagator (REQ-TSDK-01, REQ-TSDK-03, REQ-TSDK-04). `complexity: multi_file`
- [ ] 1.4 Implement disabled-mode no-op path: when `enabled=false`, install SDK no-op providers and skip exporter dial (REQ-TSDK-02, D5). `complexity: single_file`
- [ ] 1.5 Implement `Shutdown(ctx)`: flush trace then metric providers, make idempotent with a `sync.Once` guard (REQ-TSDK-04). `complexity: single_file`
- [ ] 1.6 **Test SCEN-TSDK-01** `TestInit_RegistersProviders` — assert globals are SDK providers (not no-op) after Init. `complexity: single_file`
- [ ] 1.7 **Test SCEN-TSDK-02** `TestInit_Disabled_NoOp` — assert no exporter dial, no-op providers returned. `complexity: single_file`
- [ ] 1.8 **Test SCEN-TSDK-03** `TestShutdown_FlushesAndIsIdempotent` — use in-memory exporters, assert receive + double-Shutdown returns nil. `complexity: multi_file`
- [ ] 1.9 **Test SCEN-TSDK-05** `TestOTLPExporter_HostPortHTTP` — assert exporter target host + scheme http + insecure (REQ-TSDK-03). `complexity: single_file`

## 2. Config Integration

- [ ] 2.1 Add `Telemetry` struct to `internal/config` with nested `Service`, `OTLP`, `Metrics`, `Traces` sub-structs and yaml tags (REQ-TSDK-02). `complexity: single_file`
- [ ] 2.2 **Test SCEN-TSDK-04** `TestLoad_TelemetrySection` — parse a fixture `config.yaml`, assert endpoint/interval/sampling_ratio parsed (interval as `time.Duration`). `complexity: single_file`
- [ ] 2.3 Add a sample `config.yaml` telemetry block to repo docs / example config. `complexity: text_op`

## 3. Gateway Bootstrap Wiring

- [ ] 3.1 Wire `telemetry.Init` in `cmd/gateway/main.go` startup, capture `Shutdown` (REQ-TSDK-05). `complexity: single_file`
- [ ] 3.2 Wire `Shutdown` into the existing signal handler (`SIGINT`/`SIGTERM`) with a 5s drain timeout before exit (REQ-TSDK-05). `complexity: single_file`
- [ ] 3.3 **Test SCEN-TSDK-06** `TestSignalHandling_InvokesTelemetryShutdown` — send signal to test process, assert Shutdown invoked within drain window. `complexity: multi_file`

## 4. Token Usage Histogram

- [ ] 4.1 Create `internal/telemetry/attributes.go`: centralized `attribute.Key` constants for `gen_ai.system`, `gen_ai.request.model`, `gen_ai.token.type`, `gateway.tier`, `gateway.complexity` (D4 risk mitigation). `complexity: single_file`
- [ ] 4.2 Register `gen_ai.client.token.usage` int64 histogram with unit `tokens` + advice buckets (REQ-TU-01). `complexity: single_file`
- [ ] 4.3 Implement `RecordTokenUsage(ctx, attrs TokenUsageAttrs, tokens int64)` with unknown-attr fallback to `"unknown"` (REQ-TU-02, REQ-TU-04). `complexity: single_file`
- [ ] 4.4 **Test SCEN-TU-04** `TestTokenUsageHistogram_Registration` — manual reader inspect name/unit/buckets. `complexity: single_file`
- [ ] 4.5 **Test SCEN-TU-05** `TestRecordTokenUsage_UnknownAttrFallback` — omit tier, assert value `"unknown"` not empty. `complexity: single_file`

## 5. Operation Duration Histogram

- [ ] 5.1 Register `gen_ai.client.operation.duration` float64 histogram with unit `s` + advice buckets (REQ-OD-01). `complexity: single_file`
- [ ] 5.2 Implement `RecordOperationDuration(ctx, attrs OpAttrs, seconds float64)` (REQ-OD-02). `complexity: single_file`
- [ ] 5.3 **Test SCEN-OD-03** `TestDurationHistogram_Registration` — manual reader inspect name/unit/buckets. `complexity: single_file`

## 6. Tier Budget Gauge

- [ ] 6.1 Add read-only accessor `policy.RemainingByTier() map[string]int64` (and a way to know which tiers are unlimited) without mutating enforcement logic (REQ-BG-02, D2). `complexity: single_file`
- [ ] 6.2 Register `gateway.tier.budget.remaining` int64 observable gauge with callback calling `policy.RemainingByTier()`, omitting unlimited tiers (REQ-BG-01, REQ-BG-03). `complexity: multi_file`
- [ ] 6.3 **Test SCEN-BG-05** `TestBudgetGauge_Registration` — manual reader inspect name/unit. `complexity: single_file`
- [ ] 6.4 **Test SCEN-BG-01** `TestBudgetGauge_ReflectsPolicy` — two tiers, assert two data points with correct values. `complexity: single_file`
- [ ] 6.5 **Test SCEN-BG-02** `TestBudgetGauge_UpdatesAfterConsumption` — decrement via fake completion, re-collect, assert new value. `complexity: multi_file`
- [ ] 6.6 **Test SCEN-BG-03** `TestBudgetGauge_UnlimitedTierOmitted` — unlimited tier produces no observation. `complexity: single_file`
- [ ] 6.7 **Test SCEN-BG-04** `TestBudgetGauge_NoTiers` — zero tiers → no observations, no error (REQ-BG-05). `complexity: single_file`

## 7. Trace Instrumentation

- [ ] 7.1 Expose `telemetry.Tracer() trace.Tracer` backed by a cached named tracer with instrumentation version (REQ-TR-01). `complexity: single_file`
- [ ] 7.2 Wrap `modelmanager.Complete` with a child span `modelmanager.Complete`: set attributes, events `llm.completion.start`/`llm.completion.end`, `RecordError`+`SetStatus` on error (REQ-TR-03, D7). `complexity: multi_file`
- [ ] 7.3 Wrap `rest` /implement handler with span `gateway.rest.implement`: set http.* attributes, set gateway.tier/complexity/model post-routing via `SetAttributes`, set final status code (REQ-TR-02). `complexity: multi_file`
- [ ] 7.4 **Test SCEN-TR-01** `TestImplementHandler_CreatesSpan` — tracetest exporter, assert name + http.* + tier attrs. `complexity: single_file`
- [ ] 7.5 **Test SCEN-TR-02** `TestImplementHandler_ErrorSpan` — force 500, assert span status Error + recorded error + status code. `complexity: single_file`
- [ ] 7.6 **Test SCEN-TR-03** `TestComplete_ChildSpanOfRest` — assert shared trace ID + parent/child relationship. `complexity: multi_file`
- [ ] 7.7 **Test SCEN-TR-04** `TestComplete_RecordsCompletionEvents` — assert both span events with token payloads. `complexity: single_file`
- [ ] 7.8 **Test SCEN-TR-05** `TestComplete_ErrorSpan` — upstream error → RecordError + status Error + span ended. `complexity: single_file`
- [ ] 7.9 **Test SCEN-TR-06** `TestImplementHandler_NoOpWhenDisabled` — disabled config → no-op span, IsRecording()==false, exporter empty (REQ-TR-04). `complexity: single_file`
- [ ] 7.10 **Test SCEN-TR-07** `TestImplementHandler_ContractUnchanged` — byte-identical response baseline vs instrumented (REQ-TR-05, D8). `complexity: multi_file`

## 8. modelmanager Metric Emission (integration of §4+§5+§7)

- [ ] 8.1 In `modelmanager.Complete`: capture `start := time.Now()` before upstream call; after return record duration (success+failure) and token usage (success only) (REQ-TU-03, REQ-OD-03, D7). `complexity: multi_file`
- [ ] 8.2 **Test SCEN-TU-01** `TestComplete_RecordsTokenUsage` — input+output recorded with full attribute set. `complexity: single_file`
- [ ] 8.3 **Test SCEN-TU-02** `TestComplete_RecordsReasoningTokens` — reasoning token.type recorded when present. `complexity: single_file`
- [ ] 8.4 **Test SCEN-TU-03** `TestComplete_Error_NoTokenRecord` — error path records no token usage. `complexity: single_file`
- [ ] 8.5 **Test SCEN-OD-01** `TestComplete_RecordsDuration_Success` — ~250ms stub, assert duration in band + attrs match token attrs (REQ-OD-04). `complexity: single_file`
- [ ] 8.6 **Test SCEN-OD-02** `TestComplete_RecordsDuration_Failure` — error path still records duration. `complexity: single_file`
- [ ] 8.7 **Test SCEN-OD-04** `TestComplete_DurationExcludesQueue` — pre-call delay excluded, only provider latency recorded. `complexity: multi_file`

## 9. Dev Backend (Aspire Dashboard)

- [ ] 9.1 Add `docker-compose.aspire.yml` starting `mcr.microsoft.com/dotnet/nightly/aspire-dashboard`, OTLP HTTP `:4318`, UI `:18888` (REQ-TSDK-06). `complexity: single_file`
- [ ] 9.2 Add `make dev-telemetry` target: `docker compose -f docker-compose.aspire.yml up -d` + print dashboard URL. `complexity: text_op`
- [ ] 9.3 **SCEN-TSDK-07** smoke check: document a manual run that Aspire receives metrics+traces and UI is reachable; add as a documented manual test step in `specs/telemetry-sdk.md`. `complexity: text_op`

## 10. Cross-cutting / Polish

- [ ] 10.1 Run `go vet ./...` and `go test ./...` green with race detector (`-race`). `complexity: text_op`
- [ ] 10.2 Add a short `docs/observability.md` describing local dev flow + prod Grafana note (D6). `complexity: text_op`
- [ ] 10.3 Verify all five attribute names appear only in `telemetry/attributes.go` (grep guard) to validate D4. `complexity: text_op`
