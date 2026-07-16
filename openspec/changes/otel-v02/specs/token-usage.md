# Spec: Token Usage Histogram

## Purpose

Record per-completion token consumption as a histogram so operators can compare
input/output/reasoning token spend across tiers, models, and complexity bands.

## Instrument

| Property | Value |
|---|---|
| Name | `gen_ai.client.token.usage` |
| Kind | Histogram (synchronous) |
| Unit | `tokens` (dimensionless count) |
| Temporality | Cumulative (OTLP default) |
| Advice bucket boundaries | `{1, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384}` |

## Attributes

| Attribute | Source | Example |
|---|---|---|
| `gen_ai.system` | provider key in modelmanager | `"openai"`, `"anthropic"`, `"local"` |
| `gen_ai.request.model` | resolved model id | `"gpt-4o-mini"` |
| `gen_ai.token.type` | computed per record | `"input"`, `"output"`, `"reasoning"` |
| `gateway.tier` | router tier label | `"local"`, `"remote-fast"`, `"remote-quality"` |
| `gateway.complexity` | policy-classified band | `"simple"`, `"moderate"`, `"complex"` |

> **Note (Risk: MEDIUM):** GenAI semconv is still Development status; attribute
> names are versioned but may shift. These names MUST be centralized in a single
> `attribute` constant block so renaming is a one-file change.

## Requirements

### REQ-TU-01 — Instrument Registration

`internal/telemetry` SHALL register exactly one histogram named
`gen_ai.client.token.usage` with `unit = "tokens"` and the advice buckets above.

- MUST be created via `meter.Int64Histogram(name, ...)`.
- Registration MUST be safe to call once at `Init` and cached on the telemetry
  handle.

### REQ-TU-02 — Recording API

The package SHALL expose `RecordTokenUsage(ctx, attrs TokenUsageAttrs, tokens int64)`
where `attrs` carries all five attributes.

- MUST build the attribute set from `attrs` via `attribute.KeyValue`.
- When telemetry is disabled (no-op meter), `RecordTokenUsage` MUST be a no-op
  with zero allocation on the hot path beyond the function call.

### REQ-TU-03 — Emission Point

`modelmanager.Complete` MUST call `RecordTokenUsage` exactly once per distinct
`gen_ai.token.type` present in the completion response (typically `input` and
`output`; `reasoning` only when the provider returns reasoning tokens).

- Record MUST happen after the upstream completion returns and only on success
  (non-error) completions. Failed completions MUST NOT record token usage.

### REQ-TU-04 — Attribute Completeness

Every recorded data point MUST carry all five attributes with non-empty string
values. If any attribute is unknown at record time, it MUST be recorded as the
literal `"unknown"` rather than omitted, so cardinality stays bounded.

## Scenarios

### SCEN-TU-01 — Successful completion records input + output tokens

```gherkin
GIVEN a modelmanager.Complete call that returns usage{InputTokens: 120, OutputTokens: 48}
 AND the router selected tier="remote-fast", complexity="simple", model="gpt-4o-mini"
WHEN the completion returns successfully
THEN RecordTokenUsage is called for token.type="input" with value 120
 AND RecordTokenUsage is called for token.type="output" with value 48
 AND each record carries gen_ai.system, gen_ai.request.model, gateway.tier, gateway.complexity
```
**Test:** `modelmanager_metrics_test.go::TestComplete_RecordsTokenUsage`

### SCEN-TU-02 — Reasoning tokens recorded when present

```gherkin
GIVEN a completion response that includes reasoning tokens = 32
WHEN Complete returns
THEN RecordTokenUsage is called with token.type="reasoning" and value 32
```
**Test:** `modelmanager_metrics_test.go::TestComplete_RecordsReasoningTokens`

### SCEN-TU-03 — Failed completion records nothing

```gherkin
GIVEN a Complete call whose upstream provider returns an error
WHEN the error propagates
THEN RecordTokenUsage is not called at all
```
**Test:** `modelmanager_metrics_test.go::TestComplete_Error_NoTokenRecord`

### SCEN-TU-04 — Histogram registered with correct metadata

```gherkin
GIVEN an initialized telemetry handle
WHEN the meter is inspected (via a test exporter / in-memory reader)
THEN a histogram instrument exists named "gen_ai.client.token.usage"
 AND its unit is "tokens"
 AND the advice bucket boundaries match the spec
```
**Test:** `telemetry_test.go::TestTokenUsageHistogram_Registration`

### SCEN-TU-05 — Unknown attribute becomes "unknown"

```gherkin
GIVEN a completion where the tier cannot be resolved (transient gap)
WHEN RecordTokenUsage is called
THEN the gateway.tier attribute value is "unknown" (never empty)
```
**Test:** `telemetry_test.go::TestRecordTokenUsage_UnknownAttrFallback`
