# Spec: Cost-Per-Artifact Telemetry

## Overview

Expand the existing per-call token telemetry into a holistic cost-per-artifact
view. Track retries, QA failures, schema violations, tool errors and recoveries,
trace-context linkage, and a derived dollar-cost estimate driven by a
configurable price table.

## Requirements

### R1 — gateway.task.attempts Counter (Gateway Side)

The gateway SHALL emit a `gateway.task.attempts` counter that increments
once per attempt (initial call + each retry) for every task it dispatches.

- Counter dimensions: `task_id`, `tier`, `model`, `attempt_type`
  (`initial` | `retry`).
- The counter MUST be incremented before the remote/local call is made,
  not after, so that a hung call still records the attempt.

### R2 — gateway.task.qa_failures Counter (Mastra Side)

The Mastra workflow layer SHALL emit a `gateway.task.qa_failures` counter
each time a QA validation step rejects the produced artifact.

- Counter dimensions: `task_id`, `tier`, `model`, `qa_rule`
  (the rule identifier that triggered the rejection).

### R3 — gateway.task.tokens_total Histogram (Mastra Side)

The Mastra workflow layer SHALL emit a `gateway.task.tokens_total` histogram
that is **cumulative across retries** for a given artifact.

- For a task that required N attempts, the histogram records the **sum**
  of `prompt_tokens + completion_tokens` across all N attempts — including
  failed attempts that consumed tokens (e.g., a 200 response that later
  failed QA).
- This is distinct from the RemoteResult token counts in the remote-client
  spec, which reflect only the final successful call. The telemetry layer
  reconciles both: the histogram captures total spend, the RemoteResult
  captures final-call spend.
- Histogram bucket boundaries (tokens):
  `[0, 500, 1 000, 2 500, 5 000, 10 000, 25 000, 50 000, 100 000,
  +Inf]`.
- Dimensions: `task_id`, `tier`, `model`.

### R4 — gateway.task.accepted Counter (Mastra Side)

The Mastra workflow layer SHALL emit a `gateway.task.accepted` counter when
an artifact passes QA and is committed.

- Dimensions: `task_id`, `tier`, `model`.

### R5 — gateway.task.schema_violations Counter (Mastra Side)

The Mastra workflow layer SHALL emit a `gateway.task.schema_violations`
counter whenever a Zod schema parse fails on an upstream response.

- Dimensions: `task_id`, `tier`, `model`, `zod_issue`
  (the first Zod issue code, e.g., `invalid_type`).

### R6 — gateway.task.tool_failures / tool_recoveries Counters (Mastra Side)

The Mastra workflow layer SHALL emit:

- `gateway.task.tool_failures` — incremented when a tool invocation throws
  or returns an error status.
- `gateway.task.tool_recoveries` — incremented when the workflow retries a
  failed tool invocation and the retry succeeds.

- Dimensions for both: `task_id`, `tier`, `model`, `tool_name`.

### R7 — W3C Trace Context Propagation

The gateway SHALL generate a W3C `traceparent` header
(`00-<trace-id>-<span-id>-<flags>`) for each inbound task and forward it
to the Mastra workflow layer.

The Mastra workflow layer MUST:
- Read the `traceparent` header from the inbound request.
- Create a child span linked to the propagated trace context.
- Forward the `traceparent` on any downstream calls it makes back to
  the gateway (e.g., the commit callback).

If `traceparent` is absent, the workflow layer SHALL start a new root
trace and record a `trace.orphaned` event so the gap is visible.

### R8 — Price Table in config.yaml

A `price_per_million_tokens` map SHALL be added to `config.yaml` keyed
by tier name:

```yaml
price_per_million_tokens:
  local_fast:
    input: 0.0
    output: 0.0
  local_reasoning:
    input: 0.0
    output: 0.0
  remote_deepseek:
    input: 0.14
    output: 0.28
  remote_glm:
    input: 0.50
    output: 1.50
```

- The price table MUST be loaded at startup and re-readable via a config
  reload signal (SIGHUP).
- Tiers absent from the table SHALL default to `input: 0.0, output: 0.0`.

### R9 — gateway.task.cost_estimate Derived Metric

The telemetry layer SHALL compute and emit a `gateway.task.cost_estimate`
gauge per committed artifact:

```
cost_estimate_usd =
  (tokens_total.prompt × price.input / 1_000_000)
  + (tokens_total.completion × price.output / 1_000_000)
```

- `tokens_total` here is the cumulative histogram value from R3, not the
  final-call value.
- Dimensions: `task_id`, `tier`, `model`.
- The metric SHALL be emitted at commit time alongside the
  `gateway.task.accepted` counter.

### R10 — Metric Naming and Instrumentation Library

All metrics SHALL use the `gateway.task.*` prefix and MUST be registered
through a single `telemetry.Meter` instance to avoid duplicate-registration
panics. The telemetry layer SHALL use the OpenTelemetry Metrics SDK.

## Scenarios

### S1 — attempts counter increments per dispatch

```gherkin
GIVEN a task is dispatched to "remote_deepseek"
WHEN the gateway makes the initial remote call
THEN gateway.task.attempts SHALL increment by 1 with attempt_type="initial"
AND when the call is retried once gateway.task.attempts SHALL increment by 1 with attempt_type="retry"
```

### S2 — qa_failures counter increments on QA rejection

```gherkin
GIVEN the Mastra workflow produced an artifact
WHEN the QA step rejects the artifact for rule "file_naming"
THEN gateway.task.qa_failures SHALL increment by 1 with qa_rule="file_naming"
```

### S3 — tokens_total is cumulative across retries

```gherkin
GIVEN a task's first attempt consumed 1 000 tokens and failed QA
AND the second attempt consumed 1 200 tokens and passed QA
WHEN the histogram records the artifact
THEN gateway.task.tokens_total SHALL observe the value 2 200
```

### S4 — accepted counter increments on commit

```gherkin
GIVEN an artifact passed QA
WHEN the Mastra workflow commits the artifact
THEN gateway.task.accepted SHALL increment by 1
```

### S5 — schema_violations counter increments on Zod failure

```gherkin
GIVEN the Mastra workflow received a response that fails Zod validation
WHEN the Zod parse throws with issue code "invalid_type"
THEN gateway.task.schema_violations SHALL increment by 1 with zod_issue="invalid_type"
```

### S6 — tool_failures increments on tool error

```gherkin
GIVEN a tool invocation named "file_writer" throws an error
WHEN the workflow catches the error
THEN gateway.task.tool_failures SHALL increment by 1 with tool_name="file_writer"
```

### S7 — tool_recoveries increments on retry success

```gherkin
GIVEN a tool invocation failed and is retried
WHEN the retry succeeds
THEN gateway.task.tool_recoveries SHALL increment by 1 with tool_name matching the failed call
```

### S8 — traceparent forwarded to Mastra

```gherkin
GIVEN the gateway generates a traceparent header for a task
WHEN the task is sent to the Mastra workflow
THEN the request SHALL include the traceparent header
AND the Mastra workflow SHALL create a child span linked to the propagated trace
```

### S9 — orphaned trace recorded when traceparent absent

```gherkin
GIVEN an inbound request to Mastra has no traceparent header
WHEN the workflow starts processing
THEN the workflow SHALL start a new root trace
AND the workflow SHALL record a "trace.orphaned" event
```

### S10 — cost_estimate computed from cumulative tokens

```gherkin
GIVEN an artifact on tier "remote_deepseek" with cumulative prompt_tokens=5 000 and completion_tokens=3 000
WHEN the artifact is committed
THEN gateway.task.cost_estimate SHALL be emitted
AND the value SHALL be (5000 × 0.14 + 3000 × 0.28) / 1_000_000 = 0.00154 USD
```

### S11 — missing tier in price table defaults to zero

```gherkin
GIVEN a tier "experimental_model" is absent from the price table
WHEN cost_estimate is computed for that tier
THEN the price SHALL default to input=0.0 and output=0.0
AND cost_estimate SHALL be 0.0
```

### S12 — price table reload on SIGHUP

```gherkin
GIVEN the price table is loaded at startup
WHEN the process receives SIGHUP
THEN the price table SHALL be re-read from config.yaml
AND subsequent cost_estimate calculations SHALL use the updated prices
```

### S13 — duplicate metric registration does not panic

```gherkin
GIVEN the telemetry layer initializes its Meter
WHEN multiple components attempt to register the same gateway.task.* instrument
THEN the Meter SHALL return the existing instrument instance
AND the process SHALL NOT panic with a duplicate-instrument error
```

## Non-Requirements

- The telemetry layer SHALL NOT persist metrics to a database in v0.3.
  Export is via the OTLP push to the configured collector.
- The price table SHALL NOT auto-fetch live pricing from provider APIs.
  It is manually maintained.
- The system SHALL NOT alert on metric thresholds in v0.3. Alerting rules
  belong to the collector / dashboard layer.
