# Spec: Trace Instrumentation (Spans)

## Purpose

Wrap the two hot paths — `modelmanager.Complete` and the `rest /implement`
handler — with OpenTelemetry spans so distributed traces show the full
request lifecycle from HTTP entry to LLM completion.

## Span Inventory

| Span name | Created in | Parent |
|---|---|---|
| `gateway.rest.implement` | `rest` /implement handler | incoming request span (if present) |
| `modelmanager.Complete` | `modelmanager.Complete` | the rest span (when called from rest) |

## Span Attributes (shared)

`gen_ai.system`, `gen_ai.request.model`, `gateway.tier`, `gateway.complexity`

> The `/implement` span may emit `gateway.tier` and `gateway.complexity` only
> after routing selects them; pre-routing attributes are set as soon as known.

## Requirements

### REQ-TR-01 — Tracer Acquisition

`internal/telemetry` SHALL expose a single tracer handle, e.g.
`Tracer() trace.Tracer`, backed by `otel.GetTracerProvider().Tracer("go-mcp-gateway",
trace.WithInstrumentationVersion(version))`.

### REQ-TR-02 — /implement Span

The `rest` /implement handler MUST start a span named `gateway.rest.implement` at
handler entry and end it at handler exit (including the error path).

- MUST set HTTP-standard attributes where relevant (`http.method`,
  `http.route`, `http.status_code`).
- MUST set `gen_ai.request.model`, `gateway.tier`, `gateway.complexity` once
  routing resolves them — even mid-span — using `span.SetAttributes`.
- MUST record the final HTTP status code on the span before ending.
- MUST propagate the span context into the downstream `modelmanager.Complete`
  call via the request `context.Context`.

### REQ-TR-03 — modelmanager.Complete Span

`modelmanager.Complete` MUST start a child span named `modelmanager.Complete`
 scoped under the caller's span context.

- MUST set `gen_ai.system`, `gen_ai.request.model`, `gateway.tier`,
  `gateway.complexity` once the provider/model/tier are known.
- MUST record a span **event** `llm.completion.start` with the token budget
  pre-call, and `llm.completion.end` with input/output token counts post-call.
- On error, MUST call `span.RecordError(err)` and set `span.SetStatus(codes.Error, msg)`.
- MUST record the completion's wall-clock duration as the span duration (do not
  duplicate timing; reuse the same start/end as the duration histogram, REQ-OD-03).

### REQ-TR-04 — No-op When Disabled

When telemetry is disabled, spans MUST be no-op (no allocation beyond the
`trace.SpanFromContext`/start overhead), and `Tracer()` returns the SDK's no-op
tracer.

### REQ-TR-05 — No Breaking Contract

The `/implement` HTTP contract (status codes, body shape, reasoning tags) MUST
remain unchanged. Span creation MUST NOT alter response payloads or error types.

## Scenarios

### SCEN-TR-01 — /implement creates a span carrying route + status

```gherkin
GIVEN a POST /implement request with a valid body
WHEN the handler runs to completion (200)
THEN a span named "gateway.rest.implement" is created and ended
 AND it has http.method="POST", http.route="/implement", http.status_code=200
 AND it has gateway.tier and gateway.complexity set after routing
```
**Test:** `rest_trace_test.go::TestImplementHandler_CreatesSpan`

### SCEN-TR-02 — /implement error path still ends span with error status

```gherkin
GIVEN a POST /implement that triggers a downstream error (500)
WHEN the handler returns
THEN the span is ended
 AND span status is Error with the recorded error
 AND http.status_code=500 is set on the span
```
**Test:** `rest_trace_test.go::TestImplementHandler_ErrorSpan`

### SCEN-TR-03 — Complete creates a child span of the rest span

```gherkin
GIVEN a /implement request whose handler calls modelmanager.Complete
WHEN the trace is collected
THEN the modelmanager.Complete span is a child of gateway.rest.implement
 AND both spans share the same trace ID
```
**Test:** `modelmanager_trace_test.go::TestComplete_ChildSpanOfRest`

### SCEN-TR-04 — Complete span records start and end events

```gherkin
GIVEN a successful Complete call
WHEN the Complete span is collected
THEN it contains event "llm.completion.start" with the pre-call budget
 AND it contains event "llm.completion.end" with input and output token counts
```
**Test:** `modelmanager_trace_test.go::TestComplete_RecordsCompletionEvents`

### SCEN-TR-05 — Complete error records error and status

```gherkin
GIVEN a Complete call that returns an error
WHEN the span is collected
THEN RecordError was called with the error
 AND span status is Error
 AND the span is ended (not left open)
```
**Test:** `modelmanager_trace_test.go::TestComplete_ErrorSpan`

### SCEN-TR-06 — Disabled telemetry produces no-op spans

```gherkin
GIVEN telemetry disabled (no-op provider)
WHEN /implement and Complete run
THEN trace.SpanFromContext(ctx) is a no-op span
 AND IsRecording() returns false
 AND no exporter receives spans
```
**Test:** `rest_trace_test.go::TestImplementHandler_NoOpWhenDisabled`

### SCEN-TR-07 — HTTP contract unchanged with instrumentation

```gherkin
GIVEN an enabled telemetry config
WHEN a /implement request is replayed against the instrumented handler
THEN the response status, headers, and body are byte-identical to the uninstrumented baseline
```
**Test:** `rest_contract_test.go::TestImplementHandler_ContractUnchanged`
