# Design: v0.3 Remote API Clients + Cost-Per-Artifact Telemetry

## Technical Approach

### Remote API Clients

The remote client layer is structured as a single `internal/remote` package
with a shared OpenAI-compatible base client and thin provider adapters. The
base client owns the HTTP transport, request serialization, response parsing,
SSE aggregation, and 429 retry logic. Provider adapters (DeepSeek, GLM,
OpenRouter fallback) supply endpoint URLs, model-name mapping, and auth
headers.

**Why a shared base:** all three target providers (OpenRouter-hosted
DeepSeek, Z.ai GLM, OpenRouter fallback) speak the OpenAI Chat Completions
JSON schema. Duplicating HTTP plumbing three times would be wasteful and
error-prone. Per-research decision: *"OpenAI-compatible HTTP client"* is the
foundation; provider differences are isolated to the adapter layer.

**Why per-provider model-name mapping:** the research risk note calls out
that *"GLM-5.2 Z.ai endpoint differs from OpenRouter path for same model."*
Each adapter owns its own model identifier string, preventing a global
mismatch.

**Streaming vs. non-streaming:** the base client supports both. Non-streaming
is the default for single-shot completions. Streaming is used when the caller
opts in via `stream: true`. For streaming, the client reads the SSE body,
concatenates `choices[0].delta.content` fragments, and accumulates any
`usage` field that appears in the final chunk (some providers emit a usage
summary in the last `[DONE]`-adjacent chunk). This addresses the HIGH risk:
*"Remote API clients must handle streaming and non-streaming."*

**429 retry — no fallback:** per research decision, 429 handling parses
`Retry-After` and backoffs. Critically, the OpenRouter fallback is **not**
triggered on 429 because OpenRouter may itself be rate-limited. Fallback
fires only on terminal (5xx, connection-refused) errors. This mitigates the
HIGH risk: *"Upstream 429 handling must not double-deduct budget"* — see
budget safety below.

**Budget safety on retry:** failed 429 responses carry no `usage` block, so
the base client reports `RemoteResult` tokens from the **successful** call
only. The telemetry layer independently tracks **cumulative** tokens across
all attempts (including non-429 failures like QA-rejected 200s that did
consume tokens). This separation prevents the remote client from
double-deducting while still giving telemetry the full picture.

### Cost-Per-Artifact Telemetry

The telemetry layer extends the existing `internal/telemetry` package with
new OpenTelemetry instruments and a price-table-driven cost calculator.

**Cumulative vs. final-call tokens:** the `gateway.task.tokens_total`
histogram captures the *sum* of all token consumption for an artifact
across retries (from the Mastra workflow's perspective). The
`RemoteResult` token counts capture the *final successful call*. These are
two different signals for two different consumers: budget enforcement uses
final-call tokens (what the provider actually billed for the successful
response), while cost analytics uses cumulative tokens (what the artifact
truly cost including wasted attempts).

**W3C trace propagation:** the gateway generates a `traceparent` header per
inbound task and forwards it to the Mastra workflow. Mastra extracts the
trace context and creates a child span, linking gateway and Mastra traces.
If `traceparent` is absent, Mastra starts a root trace and records a
`trace.orphaned` event — making the gap visible rather than silent. This
addresses the MEDIUM risk: *"W3C trace propagation requires Mastra to
forward traceparent header."*

**Price table staleness mitigation:** the price table is loaded at startup
and reloadable via SIGHUP. A startup log line lists all tiers and their
prices so operators can visually verify currency. This mitigates the MEDIUM
risk: *"Price table goes stale"* — though the system does not auto-fetch
live pricing (non-requirement).

## Package Structure

```
internal/
  remote/
    client.go           # Base OpenAI-compatible HTTP client
    streaming.go        # SSE reader + usage aggregator
    retry.go            # 429 Retry-After parser + exponential backoff
    deepseek.go         # DeepSeek V4-Flash adapter (OpenRouter)
    glm.go              # GLM-5.2 adapter (Z.ai)
    openrouter_fallback.go   # OpenRouter fallback adapter
    types.go            # RemoteRequest, RemoteResult, error types
    resolver.go         # Tier → adapter resolution
    client_test.go
    streaming_test.go
    retry_test.go
    deepseek_test.go
    glm_test.go
    openrouter_fallback_test.go
    resolver_test.go

  telemetry/
    metrics.go          # Instrument registration (single Meter)
    counters.go         # gateway.task.* counter definitions
    histograms.go       # gateway.task.tokens_total histogram
    gauges.go           # gateway.task.cost_estimate gauge
    trace.go            # W3C traceparent extraction / injection
    price_table.go      # PriceTable load + SIGHUP reload + cost calc
    price_table_test.go
    metrics_test.go
    trace_test.go

config/
  config.yaml           # price_per_million_tokens added here

internal/
  gateway/
    rest/
      implement.go      # MODIFIED: dispatch to remote resolver
    mcp/
      route_complete.go # MODIFIED: dispatch to remote resolver
```

## Libraries and Versions

| Library | Version | Purpose |
|---|---|---|
| `go.opentelemetry.io/otel` | `v1.28.0` | Metrics SDK + trace context |
| `go.opentelemetry.io/otel/sdk` | `v1.28.0` | SDK meter/provider |
| `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp` | `v1.28.0` | OTLP metric push |
| `go.opentelemetry.io/otel/trace` | `v1.28.0` | W3C traceparent propagation |
| `nhooyr.io/websocket` or stdlib `net/http` | stdlib | HTTP client for remote providers (non-WS path uses stdlib) |
| `github.com/stretchr/testify` | `v1.9.0` | Test assertions + mocks |

All remote client HTTP calls use the Go standard library `net/http` client
with a shared `*http.Client` configured per-adapter (timeout, transport).
No third-party HTTP framework is introduced.

## Architectural Decisions

| # | Decision | Research Reference | Rationale |
|---|---|---|---|
| A1 | Single shared base client, thin per-provider adapters | *"internal/remote package — OpenAI-compatible HTTP client"* | All three providers speak OpenAI Chat Completions; avoids triplicated HTTP code. |
| A2 | Per-provider model-name mapping (not global) | Risk: *"GLM-5.2 Z.ai endpoint differs from OpenRouter path"* | GLM is `glm-5.2` on Z.ai but a different alias on OpenRouter. |
| A3 | OpenRouter fallback fires on terminal errors only, not on 429 | *"OpenRouter fallback client (single key, all remote tiers)"* + Risk: *"429 handling must not double-deduct budget"* | OpenRouter may itself be rate-limited; falling over to it on 429 could cascade. |
| A4 | RemoteResult tokens = final-call only; telemetry tokens_total = cumulative | *"tokens_total histogram cumulative across retries"* | Separates budget billing (final call) from cost analytics (all attempts). |
| A5 | 429 Retry-After: respect header, fall back to exponential backoff with 30 s cap and 3 retry max | *"Upstream 429 Retry-After parsing per provider"* | Matches provider backoff hints; bounded cap prevents indefinite waits. |
| A6 | W3C traceparent generated by gateway, forwarded to Mastra, orphan event if missing | *"W3C trace context propagation — gateway spans link to Mastra spans"* + Risk: *"requires Mastra to forward traceparent"* | Standardized trace linkage; silent fallback to root trace would hide gaps. |
| A7 | Price table in config.yaml, reloadable via SIGHUP | *"Price table in config.yaml"* + Risk: *"Price table goes stale"* | Operators can update without restart; log-on-load provides auditability. |
| A8 | cost_estimate = cumulative_tokens × tier_price | *"gateway.task.cost_estimate derived from tokens × tier price"* | Reflects true artifact cost, not just the final call's spend. |
| A9 | Single telemetry.Meter instance for all instruments | Risk: duplicate-registration panics | OpenTelemetry instruments are singletons per name; centralized registration avoids panics. |
| A10 | No tool-use / function-calling passthrough in remote clients | Non-requirement | Remote tiers produce text only in v0.3; tool-use is local-tier only. |

## Data Flow

```
┌──────────┐  traceparent  ┌──────────┐
│  Gateway │──────────────▶│  Mastra  │
│ (REST/   │               │ Workflow │
│  MCP)    │◀──────────────│          │
└────┬─────┘  commit+trace └────┬─────┘
     │                          │
     │ dispatch task            │ QA / Zod / tools
     ▼                          │
┌──────────────┐                ▼
│ remote.      │         ┌───────────────┐
│ resolver     │         │ telemetry     │
│ (tier→adapter│         │ counters /    │
└──────┬───────┘         │ histograms    │
       │                 │ gauges        │
       ▼                 └───────────────┘
┌──────────────────────────────┐
│ Base HTTP Client             │
│  ├─ DeepSeek adapter         │──▶ OpenRouter API
│  ├─ GLM adapter              │──▶ Z.ai API
│  └─ OpenRouter fallback      │──▶ OpenRouter API
│     (terminal errors only)   │
└──────────────────────────────┘
       │
       ▼ RemoteResult{content, promptTokens, completionTokens}
       │
       └─▶ telemetry records tokens_total (cumulative)
                         cost_estimate (tokens × price)
```

## Configuration

### config.yaml additions

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

remote:
  default_timeout_seconds: 120
  default_max_tokens: 8192
  max_retries_429: 3
  retry_backoff_initial_seconds: 2
  retry_backoff_cap_seconds: 30
```

### Environment variables

| Variable | Used by | Required for |
|---|---|---|
| `OPENROUTER_API_KEY` | DeepSeek adapter, OpenRouter fallback | `remote_deepseek` tier + fallback |
| `ZAI_API_KEY` | GLM adapter | `remote_glm` tier |

## Testing Strategy

- **Unit tests** for each adapter using `httptest.Server` to mock provider
  responses (200, 429, 500, streaming SSE).
- **Retry logic** tested in isolation with a fake clock to avoid real sleeps.
- **Price table** tested for load, SIGHUP reload, and missing-tier default.
- **Cost calculation** tested with known token × price combinations.
- **Trace propagation** tested by asserting `traceparent` header presence
  on forwarded requests and child-span linkage.
- **Integration test** for the REST `/implement` handler verifying a
  `multi_file` task returns real (non-stub) content from a mock provider.
