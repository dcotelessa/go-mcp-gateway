# Proposal: v0.2 OpenTelemetry Integration

## Problem
The gateway routes completions across local and remote tiers but has no
observability into token usage, latency, or cost per tier. Without this
data, model selection decisions are based on benchmarks, not real workload
performance.

## What Will Change
- ADDED: internal/telemetry package — OTel SDK setup, tracer, meter
- ADDED: gen_ai.client.token.usage histogram per tier/model/complexity
- ADDED: gen_ai.client.operation.duration histogram per tier/model/complexity  
- ADDED: gateway.tier.budget.remaining gauge per tier
- ADDED: OTLP exporter configuration in config.yaml
- MODIFIED: rest handlers — wrap /implement with spans and metrics
- MODIFIED: modelmanager.Complete — emit token usage after each completion
- MODIFIED: config — add telemetry section (endpoint, service name, opt-ins)

## What Won't Change
- All existing REST and MCP contracts
- Reasoning tags contract with Mastra workflow
- Model routing logic
- Policy/budget enforcement

## Key Risks
- (MEDIUM) GenAI semconv still Development status — attribute names may change
- (LOW) OTLP export adds ~1ms overhead per completion — negligible vs LLM latency
- (LOW) Aspire Dashboard for dev, Grafana for production — two configs to maintain
