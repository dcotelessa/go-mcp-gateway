# Spec: Tier Budget Remaining Gauge

## Purpose

Expose the remaining token budget per tier as an observable (pull-model) gauge,
so Aspire/Grafana dashboards can display live budget headroom without requiring
a push event per change.

## Instrument

| Property | Value |
|---|---|
| Name | `gateway.tier.budget.remaining` |
| Kind | Observable gauge (asynchronous, int64) |
| Unit | `tokens` |
| Callback cadence | Driven by the metric reader's collect cycle (default 15s) |

## Attributes

| Attribute | Source | Example |
|---|---|---|
| `gateway.tier` | router tier label | `"local"`, `"remote-fast"`, `"remote-quality"` |

## Requirements

### REQ-BG-01 — Instrument Registration

`internal/telemetry` SHALL register one int64 observable gauge named
`gateway.tier.budget.remaining` with `unit = "tokens"`.

- MUST be created via `meter.Int64ObservableGauge(name, metric.WithInt64Callback(cb))`.
- The callback MUST be invoked by the SDK reader during each collect cycle.

### REQ-BG-02 — Budget Source

The gauge callback SHALL read remaining budget from `internal/policy` (the
existing budget enforcement module). It MUST NOT duplicate or cache budget state
in `internal/telemetry`.

- The callback signature MUST accept an `Observer` and emit one observation per
  tier currently known to the policy module.

### REQ-BG-03 — Live Values

- Each observation MUST reflect the policy module's current remaining budget at
  collect time (no staleness beyond the reader interval).
- If a tier has no budget limit configured, its observation MUST be omitted
  (not zero, not negative) to avoid implying an exhausted budget.

### REQ-BG-04 — No Budget Drift

- The gauge MUST be read-only against policy state. Recording budget via the
  gauge MUST be impossible (observable gauges have no `Record` method).

### REQ-BG-05 — Safe When Empty

- If the policy module reports zero configured tiers at collect time, the
  callback MUST emit zero observations without error.

## Scenarios

### SCEN-BG-01 — Gauge reflects current remaining budget

```gherkin
GIVEN policy has tiers {local: remaining=1000, remote-fast: remaining=500}
 AND the telemetry gauge is wired to policy
WHEN the metric reader collects
THEN the exporter receives gateway.tier.budget.remaining with two data points:
  (gateway.tier="local", value=1000) and (gateway.tier="remote-fast", value=500)
```
**Test:** `telemetry_gauge_test.go::TestBudgetGauge_ReflectsPolicy`

### SCEN-BG-02 — Gauge updates after a completion consumes budget

```gherkin
GIVEN policy tier "local" has remaining=1000
 AND a completion consumes 100 tokens (policy decrements remaining to 900)
WHEN the next collect cycle runs
THEN the gauge emits gateway.tier="local" with value=900
```
**Test:** `telemetry_gauge_test.go::TestBudgetGauge_UpdatesAfterConsumption`

### SCEN-BG-03 — Tier without limit is omitted

```gherkin
GIVEN policy tier "remote-quality" has no budget limit configured
WHEN the reader collects
THEN no observation is emitted for gateway.tier="remote-quality"
```
**Test:** `telemetry_gauge_test.go::TestBudgetGauge_UnlimitedTierOmitted`

### SCEN-BG-04 — No tiers configured emits nothing

```gherkin
GIVEN policy reports zero configured tiers
WHEN the reader collects
THEN the callback returns no observations and no error
```
**Test:** `telemetry_gauge_test.go::TestBudgetGauge_NoTiers`

### SCEN-BG-05 — Gauge registered with tokens unit

```gherkin
GIVEN an initialized telemetry handle
WHEN the meter is inspected via an in-memory reader
THEN an observable gauge named "gateway.tier.budget.remaining" exists with unit "tokens"
```
**Test:** `telemetry_gauge_test.go::TestBudgetGauge_Registration`
