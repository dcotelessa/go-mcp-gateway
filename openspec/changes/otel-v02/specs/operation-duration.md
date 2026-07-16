# Spec: Operation Duration Histogram

## Purpose

Measure end-to-end LLM completion latency so operators can compare p50/p95/p99
across tiers, models, and complexity bands and validate routing decisions
against real workload performance.

## Instrument

| Property | Value |
|---|---|
| Name | `gen_ai.client.operation.duration` |
| Kind | Histogram (synchronous) |
| Unit | `s` (seconds) |
| Temporality | Cumulative (OTLP default) |
| Advice bucket boundaries | `{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120}` |

## Attributes

Identical attribute set to token usage:

`gen_ai.system`, `gen_ai.request.model`, `gateway.tier`, `gateway.complexity`

(Duration has no `gen_ai.token.type`.)

## Requirements

### REQ-OD-01 — Instrument Registration

`internal/telemetry` SHALL register exactly one histogram named
`gen_ai.client.operation.duration` with `unit = "s"` and the advice buckets above.

- MUST be created via `meter.Float64Histogram(name, ...)`.

### REQ-OD-02 — Recording API

The package SHALL expose `RecordOperationDuration(ctx, attrs OpAttrs, seconds float64)`.

- When telemetry is disabled, MUST be a no-op.

### REQ-OD-03 — Emission Point

`modelmanager.Complete` MUST measure wall-clock duration from just before the
upstream call to just after the response (inclusive of serialization, exclusive
of gateway queueing). It MUST call `RecordOperationDuration` exactly once on
both success and failure paths.

- On the failure path, duration MUST still be recorded (latency of a failed
  call is observable signal).

### REQ-OD-04 — Attribute Consistency

Duration records MUST use the same attribute values as the token-usage records
emitted in the same `Complete` invocation, so the two histograms are joinable
in queries.

### REQ-OD-05 — Precision

Duration MUST be recorded with at least millisecond precision
(`time.Since` → `float64(time.Duration)/float64(time.Second)`).

## Scenarios

### SCEN-OD-01 — Success records duration

```gherkin
GIVEN a Complete call that takes ~250ms and succeeds
WHEN the call returns
THEN RecordOperationDuration is called once with seconds in [0.24, 0.26]
 AND attributes match the token-usage attributes for the same call
```
**Test:** `modelmanager_metrics_test.go::TestComplete_RecordsDuration_Success`

### SCEN-OD-02 — Failure still records duration

```gherkin
GIVEN a Complete call that takes ~300ms and returns an error
WHEN the error is returned
THEN RecordOperationDuration is called once with seconds in [0.29, 0.31]
 AND token usage is NOT recorded (per SCEN-TU-03)
```
**Test:** `modelmanager_metrics_test.go::TestComplete_RecordsDuration_Failure`

### SCEN-OD-03 — Histogram registered with seconds unit and boundaries

```gherkin
GIVEN an initialized telemetry handle
WHEN the meter is inspected via an in-memory reader
THEN a histogram named "gen_ai.client.operation.duration" exists
 AND its unit is "s"
 AND advice boundaries match the spec
```
**Test:** `telemetry_test.go::TestDurationHistogram_Registration`

### SCEN-OD-04 — Duration excludes gateway queue time

```gherkin
GIVEN a 100ms artificial delay introduced before calling the provider stub
 AND a 100ms provider stub latency
WHEN Complete records duration
THEN recorded seconds are in [0.09, 0.11] (provider latency only)
```
**Test:** `modelmanager_metrics_test.go::TestComplete_DurationExcludesQueue`
