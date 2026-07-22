# Tasks: v0.3 Remote API Clients + Cost-Per-Artifact Telemetry

## 1 — Remote Client Foundation

- [ ] 1.1 Create `internal/remote/types.go` with `RemoteRequest`, `RemoteResult{content, promptTokens, completionTokens}`, `RateLimitedError`, `TerminalError` types
      Complexity: scaffold
- [ ] 1.2 Create `internal/remote/client.go` — base OpenAI-compatible HTTP client with `Do(ctx, RemoteRequest) (RemoteResult, error)`, configurable base URL, API key, model, timeout, max tokens. Non-streaming path parses `choices[0].message.content` and `usage.prompt_tokens`/`usage.completion_tokens`.
      Complexity: single_file
      Spec: remote-api-clients R1
- [ ] 1.3 Create `internal/remote/streaming.go` — SSE reader that concatenates `choices[0].delta.content` and aggregates `usage` from the final chunk. Wire into base client when `stream: true`.
      Complexity: single_file
      Spec: remote-api-clients R1, S8

## 2 — 429 Retry Logic

- [ ] 2.1 Create `internal/remote/retry.go` — `parseRetryAfter(header string, now time.Time) time.Duration` supporting delta-seconds and HTTP-date (RFC 7231 §7.1.3). Exponential backoff fallback: initial 2 s, cap 30 s, max 3 retries.
      Complexity: single_file
      Spec: remote-api-clients R5
- [ ] 2.2 Write `internal/remote/retry_test.go` — GIVEN 429 with Retry-After: 5 WHEN retry computed THEN wait 5s. GIVEN 429 without header THEN exponential 2s/4s/8s. GIVEN max retries exceeded THEN RateLimitedError.
      Complexity: single_file
      Spec: remote-api-clients S4, S5
- [ ] 2.3 Integrate retry loop into base client `Do` — on HTTP 429 call retry logic, do NOT trigger fallback. Ensure RemoteResult tokens come from the successful call only (budget safety).
      Complexity: multi_file
      Spec: remote-api-clients R5, R7, S4, S10

## 3 — Provider Adapters

- [ ] 3.1 Create `internal/remote/deepseek.go` — DeepSeek adapter: base URL `https://openrouter.ai/api/v1/chat/completions`, model `deepseek/deepseek-chat-v4-flash`, auth via `OPENROUTER_API_KEY`, sets `HTTP-Referer` and `X-Title` headers.
      Complexity: single_file
      Spec: remote-api-clients R2, S2
- [ ] 3.2 Create `internal/remote/glm.go` — GLM adapter: base URL `https://api.z.ai/api/coding/v1/chat/completions`, model `glm-5.2`, auth via `ZAI_API_KEY`.
      Complexity: single_file
      Spec: remote-api-clients R3, S3
- [ ] 3.3 Create `internal/remote/openrouter_fallback.go` — fallback adapter that maps original tier to OpenRouter-native model alias, fires only on TerminalError (not 429), uses `OPENROUTER_API_KEY`, does not recurse into itself.
      Complexity: single_file
      Spec: remote-api-clients R4, S6, S7
- [ ] 3.4 Write adapter unit tests (`deepseek_test.go`, `glm_test.go`, `openrouter_fallback_test.go`) using `httptest.Server` mocks for 200, 429, 500, and streaming responses.
      Complexity: multi_file
      Spec: remote-api-clients S2, S3, S6

## 4 — Resolver and Fail-Fast Key Validation

- [ ] 4.1 Create `internal/remote/resolver.go` — `Resolve(tier string) (Adapter, error)` mapping `remote_deepseek` → DeepSeek adapter, `remote_glm` → GLM adapter. Validate required env vars at construction; fail fast with named-variable error if missing.
      Complexity: single_file
      Spec: remote-api-clients R6, S11
- [ ] 4.2 Write `internal/remote/resolver_test.go` — GIVEN tier "remote_deepseek" WHEN resolved THEN returns DeepSeek adapter. GIVEN OPENROUTER_API_KEY unset THEN returns error naming the variable.
      Complexity: single_file
      Spec: remote-api-clients S11

## 5 — Handler Integration

- [ ] 5.1 Modify REST `/implement` handler to dispatch `multi_file` tasks to `remote.Resolver` instead of returning stub strings. Forward RemoteResult content and token counts in the response payload.
      Complexity: multi_file
      Spec: remote-api-clients R6, S1, S9
- [ ] 5.2 Modify MCP `route_complete` handler to dispatch to `remote.Resolver` for remote tiers. Forward RemoteResult content in the MCP response.
      Complexity: multi_file
      Spec: remote-api-clients R6, S9
- [ ] 5.3 Write integration test for REST `/implement` — GIVEN a `multi_file` task and a mock provider WHEN the handler is called THEN the response contains real (non-stub) content and non-zero token counts.
      Complexity: multi_file
      Spec: remote-api-clients S1

## 6 — Telemetry Instrument Registration

- [ ] 6.1 Create `internal/telemetry/metrics.go` — single `Meter` instance with `getOrCreateCounter(name, desc, unit)` / `getOrCreateHistogram` / `getOrCreateGauge` helpers that return existing instruments to avoid duplicate-registration panics.
      Complexity: single_file
      Spec: cost-per-artifact-telemetry R10, S13
- [ ] 6.2 Create `internal/telemetry/counters.go` — define `gateway.task.attempts`, `gateway.task.qa_failures`, `gateway.task.accepted`, `gateway.task.schema_violations`, `gateway.task.tool_failures`, `gateway.task.tool_recoveries` with their dimension keys.
      Complexity: single_file
      Spec: cost-per-artifact-telemetry R1, R2, R4, R5, R6
- [ ] 6.3 Create `internal/telemetry/histograms.go` — define `gateway.task.tokens_total` with bucket boundaries `[0, 500, 1000, 2500, 5000, 10000, 25000, 50000, 100000, +Inf]` and dimensions.
      Complexity: single_file
      Spec: cost-per-artifact-telemetry R3
- [ ] 6.4 Create `internal/telemetry/gauges.go` — define `gateway.task.cost_estimate` gauge with dimensions.
      Complexity: single_file
      Spec: cost-per-artifact-telemetry R9

## 7 — Telemetry Emission Points

- [ ] 7.1 Add `gateway.task.attempts` emission in gateway dispatch — increment before each remote/local call with `attempt_type` = `initial` or `retry`.
      Complexity: multi_file
      Spec: cost-per-artifact-telemetry R1, S1
- [ ] 7.2 Add `gateway.task.qa_failures` emission in Mastra QA step — increment on rejection with `qa_rule` dimension.
      Complexity: multi_file
      Spec: cost-per-artifact-telemetry R2, S2
- [ ] 7.3 Add `gateway.task.tokens_total` emission in Mastra — accumulate prompt+completion tokens across all attempts (including non-429 failures) and observe the cumulative sum at artifact completion.
      Complexity: multi_file
      Spec: cost-per-artifact-telemetry R3, S3
- [ ] 7.4 Add `gateway.task.accepted` emission on commit in Mastra.
      Complexity: single_file
      Spec: cost-per-artifact-telemetry R4, S4
- [ ] 7.5 Add `gateway.task.schema_violations` emission on Zod parse failure in Mastra — record first issue code as `zod_issue`.
      Complexity: multi_file
      Spec: cost-per-artifact-telemetry R5, S5
- [ ] 7.6 Add `gateway.task.tool_failures` and `gateway.task.tool_recoveries` emission in Mastra tool-execution layer.
      Complexity: multi_file
      Spec: cost-per-artifact-telemetry R6, S6, S7
- [ ] 7.7 Write `internal/telemetry/metrics_test.go` — GIVEN duplicate instrument registration WHEN two components register the same name THEN the same instance is returned and no panic occurs.
      Complexity: single_file
      Spec: cost-per-artifact-telemetry S13

## 8 — W3C Trace Context Propagation

- [ ] 8.1 Create `internal/telemetry/trace.go` — `InjectTraceparent(ctx) context.Context` (gateway side) generates and injects W3C traceparent; `ExtractTraceparent(ctx) (context.Context, bool)` (Mastra side) parses and creates child span; records `trace.orphaned` event when absent.
      Complexity: single_file
      Spec: cost-per-artifact-telemetry R7
- [ ] 8.2 Wire `InjectTraceparent` into gateway task dispatch so every inbound task carries a traceparent header.
      Complexity: multi_file
      Spec: cost-per-artifact-telemetry R7, S8
- [ ] 8.3 Wire `ExtractTraceparent` into Mastra workflow entry point — create child span from propagated context; fall back to root trace + `trace.orphaned` event when missing.
      Complexity: multi_file
      Spec: cost-per-artifact-telemetry R7, S8, S9
- [ ] 8.4 Write `internal/telemetry/trace_test.go` — GIVEN traceparent present THEN child span linked. GIVEN traceparent absent THEN root trace + orphan event recorded.
      Complexity: single_file
      Spec: cost-per-artifact-telemetry S8, S9

## 9 — Price Table and Cost Estimate

- [ ] 9.1 Add `price_per_million_tokens` and `remote` config blocks to `config/config.yaml`.
      Complexity: text_op
      Spec: cost-per-artifact-telemetry R8
- [ ] 9.2 Create `internal/telemetry/price_table.go` — `LoadPriceTable(path) (PriceTable, error)` loads YAML map; `PriceTable.ForTier(tier) (inputUSD, outputUSD)` with zero-default for missing tiers; SIGHUP handler triggers reload.
      Complexity: single_file
      Spec: cost-per-artifact-telemetry R8, R9, S11, S12
- [ ] 9.3 Create `internal/telemetry/price_table_test.go` — GIVEN deepseek tier prices THEN ForTier returns 0.14/0.28. GIVEN missing tier THEN returns 0.0/0.0. GIVEN SIGHUP THEN table reloaded.
      Complexity: single_file
      Spec: cost-per-artifact-telemetry S11, S12
- [ ] 9.4 Create cost-estimate computation: `cost_estimate_usd = (prompt_tokens × input + completion_tokens × output) / 1_000_000` using cumulative tokens_total and tier prices. Emit `gateway.task.cost_estimate` gauge at commit time alongside `accepted` counter.
      Complexity: multi_file
      Spec: cost-per-artifact-telemetry R9, S10
- [ ] 9.5 Write cost-estimate test — GIVEN remote_deepseek, cumulative prompt=5000, completion=3000 WHEN committed THEN cost_estimate = 0.00154.
      Complexity: single_file
      Spec: cost-per-artifact-telemetry S10

## 10 — End-to-End Validation

- [ ] 10.1 Write E2E test: `multi_file` task dispatched through REST handler → mock DeepSeek returns content → telemetry records attempts(1), tokens_total, accepted(1), cost_estimate. Verify no stub strings in response.
      Complexity: multi_file
      Spec: remote-api-clients S1, S10; cost-per-artifact-telemetry S1, S3, S4, S10
- [ ] 10.2 Write E2E test: GLM adapter returns 500 → OpenRouter fallback succeeds → traceparent propagated end-to-end → telemetry records attempts(2), tokens_total cumulative.
      Complexity: multi_file
      Spec: remote-api-clients S6; cost-per-artifact-telemetry S1, S3, S8
- [ ] 10.3 Write E2E test: 429 × 3 on DeepSeek → RateLimitedError returned → fallback NOT triggered → attempts counter = 4 (1 initial + 3 retries) → no double-deducted budget.
      Complexity: multi_file
      Spec: remote-api-clients S4, S7, S10; cost-per-artifact-telemetry S1
