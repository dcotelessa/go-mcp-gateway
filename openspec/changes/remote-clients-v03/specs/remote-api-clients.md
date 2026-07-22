# Spec: Remote API Clients

## Overview

Replace stub string handlers for the `multi_file` complexity tier with real
OpenAI-compatible HTTP clients that call DeepSeek V4-Flash (via OpenRouter)
and GLM-5.2 (via Z.ai). Provide a shared base client, provider-specific
adapters, and robust 429 retry handling.

## Requirements

### R1 — Base HTTP Client

The system SHALL provide an OpenAI-compatible HTTP client in
`internal/remote` that supports both streaming (`stream: true`) and
non-streaming response modes.

The base client MUST:
- Accept a configurable base URL, API key, and model identifier.
- Serialize requests using the OpenAI Chat Completions JSON schema
  (`model`, `messages`, `temperature`, `max_tokens`, `stream`).
- Parse the `usage.prompt_tokens` and `usage.completion_tokens` fields
  from non-streaming responses.
- Aggregate `usage` deltas from SSE chunks in streaming responses.
- Return a structured `RemoteResult{ content string, promptTokens int,
  completionTokens int }` value to callers.

The base client SHOULD set a default timeout of 120 seconds and a default
max-token budget of 8 192 unless overridden per provider.

### R2 — DeepSeek V4-Flash via OpenRouter

The system SHALL provide a DeepSeek adapter that targets the OpenRouter
endpoint (`https://openrouter.ai/api/v1/chat/completions`).

- The model identifier sent to OpenRouter SHALL be
  `deepseek/deepseek-chat-v4-flash`.
- The adapter MUST forward the `Authorization: Bearer <key>` header using
  the `OPENROUTER_API_KEY` environment variable.
- The adapter SHOULD set `HTTP-Referer` and `X-Title` headers per the
  OpenRouter attribution convention.

### R3 — GLM-5.2 via Z.ai

The system SHALL provide a GLM adapter that targets the Z.ai Coding Plan
endpoint (`https://api.z.ai/api/coding/v1/chat/completions`).

- The model identifier sent to Z.ai SHALL be `glm-5.2` (provider-local
  name, not the OpenRouter alias).
- The adapter MUST forward the `Authorization: Bearer <key>` header using
  the `ZAI_API_KEY` environment variable.
- Because the Z.ai endpoint path differs from OpenRouter, model-name
  mapping MUST be per-provider, not global.

### R4 — OpenRouter Fallback

The system SHALL provide an OpenRouter fallback client usable for all
remote tiers. When a primary provider (DeepSeek adapter or GLM adapter)
returns a terminal error (non-429, non-timeout), the fallback client
SHALL retry the request through OpenRouter using the OpenRouter-native
model alias for that tier.

The fallback client MUST:
- Use the same `OPENROUTER_API_KEY`.
- Map the original tier to the correct OpenRouter model alias.
- Not enter an infinite loop — the fallback call is terminal and its
  error propagates to the caller if it also fails.

### R5 — 429 Retry-After Handling

When a provider returns HTTP 429, the client SHALL:
- Parse the `Retry-After` response header (delta-seconds or HTTP-date
  format per RFC 7231 §7.1.3).
- If `Retry-After` is absent, fall back to an exponential backoff
  starting at 2 s with a 30 s cap.
- Wait the computed duration, then retry.
- Retry at most 3 times before returning a `RateLimitedError` to the
  caller.

The retry logic MUST be per-provider (each adapter has its own limiter
state) and MUST NOT trigger the OpenRouter fallback on 429 — the fallback
is reserved for terminal errors only, because OpenRouter may also be
rate-limited.

### R6 — Handler Integration

The REST `/implement` handler and the MCP `route_complete` handler SHALL
dispatch `multi_file` tasks to the resolved remote client (DeepSeek or
GLM based on tier routing) instead of returning stub strings.

- The returned `content` field from the remote client SHALL populate the
  handler's response payload.
- The `promptTokens` and `completionTokens` from the `RemoteResult` SHALL
  be forwarded to the telemetry layer for cost calculation.

### R7 — Budget Safety on Retry

When a 429 retried call eventually succeeds, the client MUST report the
token usage of the **successful** call only, not the sum of failed
attempts. This prevents double-deduction: failed 429 responses typically
contain no `usage` block.

## Scenarios

### S1 — Multi-file task returns real completion

```gherkin
GIVEN a task classified as "multi_file" routed to "remote_deepseek"
WHEN the REST /implement handler invokes the remote client
THEN the response payload SHALL contain the completion text from DeepSeek
AND the payload SHALL contain non-zero prompt and completion token counts
AND the payload SHALL NOT contain any stub or placeholder string
```

### S2 — DeepSeek request via OpenRouter succeeds

```gherkin
GIVEN the OPENROUTER_API_KEY environment variable is set
WHEN the DeepSeek adapter sends a chat completion request
THEN the request SHALL be POSTed to "https://openrouter.ai/api/v1/chat/completions"
AND the model field SHALL be "deepseek/deepseek-chat-v4-flash"
AND the response content SHALL be parsed from choices[0].message.content
```

### S3 — GLM-5.2 request via Z.ai succeeds

```gherkin
GIVEN the ZAI_API_KEY environment variable is set
WHEN the GLM adapter sends a chat completion request
THEN the request SHALL be POSTed to "https://api.z.ai/api/coding/v1/chat/completions"
AND the model field SHALL be "glm-5.2" (not the OpenRouter alias)
AND the response content SHALL be parsed from choices[0].message.content
```

### S4 — 429 triggers Retry-After backoff

```gherkin
GIVEN the upstream provider returns HTTP 429 with "Retry-After: 5"
WHEN the remote client processes the response
THEN the client SHALL wait 5 seconds
AND the client SHALL retry the request
AND the client SHALL retry at most 3 times before returning a RateLimitedError
```

### S5 — 429 with missing Retry-After uses exponential backoff

```gherkin
GIVEN the upstream provider returns HTTP 429 without a Retry-After header
WHEN the remote client processes the response
THEN the client SHALL wait 2 seconds on the first retry
AND the client SHALL apply exponential backoff with a 30-second cap
```

### S6 — OpenRouter fallback on terminal error

```gherkin
GIVEN the GLM adapter returns a terminal 500 error
WHEN the fallback layer catches the error
THEN the OpenRouter fallback client SHALL retry the same request
AND the fallback SHALL use the OpenRouter-native model alias for the tier
AND if the fallback also fails the error SHALL propagate to the caller
```

### S7 — 429 does not trigger OpenRouter fallback

```gherkin
GIVEN the GLM adapter exhausts its 3 retry limit on HTTP 429
WHEN the RateLimitedError is returned
THEN the OpenRouter fallback SHALL NOT be invoked
AND the RateLimitedError SHALL propagate to the caller
```

### S8 — Streaming response aggregates usage

```gherkin
GIVEN a request is sent with "stream: true"
WHEN the upstream returns SSE chunks
THEN the client SHALL concatenate content deltas from each chunk
AND the client SHALL aggregate usage.token deltas into a final total
AND the RemoteResult SHALL reflect the full accumulated token counts
```

### S9 — MCP handler uses real remote client

```gherkin
GIVEN a task routed via the MCP route_complete handler to "remote_glm"
WHEN the handler resolves the client for the tier
THEN the handler SHALL invoke the GLM adapter
AND the handler response SHALL contain real completion content
```

### S10 — Budget not double-deducted on successful retry

```gherkin
GIVEN a call receives two 429s then succeeds on the third attempt
WHEN the RemoteResult is returned
THEN the token counts SHALL reflect only the third (successful) attempt
AND the token counts SHALL NOT include usage from the failed 429 responses
```

### S11 — Missing API key fails fast

```gherkin
GIVEN the required API key environment variable for a provider is unset
WHEN the adapter attempts to construct its client
THEN the adapter SHALL return an error immediately
AND the error message SHALL name the missing environment variable
```

## Non-Requirements

- The clients SHALL NOT implement function-calling / tool-use passthrough
  in v0.3. Remote tiers produce text completions only.
- The clients SHALL NOT cache responses. Caching is a future concern.
- The clients SHALL NOT implement request-level circuit breaking beyond
  the 429 retry logic described in R5.
