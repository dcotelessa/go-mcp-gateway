# Proposal: v0.3 Remote API Clients + Cost-Per-Artifact Telemetry

## Problem

Two gaps block the pipeline from being fully useful:

1. **Remote tiers return placeholders.** The `multi_file` complexity tier
   routes to `remote_deepseek` or `remote_glm` but the handlers return
   stub strings instead of real completions. Implementation agents get
   no actual output for multi-file tasks.

2. **Token cost is an incomplete signal.** Per-call token counts miss
   the real cost of a model: how many retries it needed, how often QA
   failed, and whether it recovered from tool errors. A cheaper model
   that requires 3 retries may cost more than an expensive model that
   completes in one pass.

## What Will Change

### Remote API clients
- ADDED: `internal/remote` package — OpenAI-compatible HTTP client
- ADDED: DeepSeek V4-Flash client via OpenRouter ($0.14/M input)
- ADDED: GLM-5.2 client via Z.ai Coding Plan endpoint
- ADDED: OpenRouter fallback client (single key, all remote tiers)
- ADDED: Upstream 429 Retry-After parsing per provider
- MODIFIED: rest /implement handler — routes multi_file to real remote client
- MODIFIED: mcp route_complete handler — same

### Cost-per-artifact telemetry
- ADDED: `gateway.task.attempts` counter (gateway side)
- ADDED: `gateway.task.qa_failures` counter (Mastra side)
- ADDED: `gateway.task.tokens_total` histogram cumulative across retries (Mastra)
- ADDED: `gateway.task.accepted` counter on commit (Mastra)
- ADDED: `gateway.task.schema_violations` counter on Zod parse failure (Mastra)
- ADDED: `gateway.task.tool_failures` / `gateway.task.tool_recoveries` (Mastra)
- ADDED: W3C trace context propagation — gateway spans link to Mastra spans
- ADDED: Price table in config.yaml (price_per_million_tokens per tier)
- ADDED: `gateway.task.cost_estimate` derived from tokens × tier price

## What Won't Change
- REST and MCP contract shapes
- Reasoning tags contract with Mastra workflow
- Routing logic and fallback cascade
- Policy/budget enforcement
- Local model management

## Key Risks
- (HIGH) Remote API clients must handle streaming and non-streaming
- (HIGH) Upstream 429 handling must not double-deduct budget
- (MEDIUM) W3C trace propagation requires Mastra to forward traceparent header
- (MEDIUM) Price table goes stale — providers change pricing regularly
- (LOW) GLM-5.2 Z.ai endpoint differs from OpenRouter path for same model
