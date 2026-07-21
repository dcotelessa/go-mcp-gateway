# Observability

The gateway emits OpenTelemetry metrics and traces following the
[GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/)
(currently Development status — attribute names centralised in
`internal/telemetry/attributes.go` for easy migration).

## Local development

Start the Aspire Dashboard (free, no account required):

```bash
make dev-telemetry
```

Open `http://localhost:18888` in your browser.

Enable telemetry in `config.yaml`:

```yaml
telemetry:
  enabled: true
  otlp:
    endpoint: http://localhost:4318
    insecure: true
  metrics:
    export_interval_sec: 15
  traces:
    sampling_ratio: 1.0
```

Restart the gateway:

```bash
./bin/gateway -config config.yaml
```

Send a request:

```bash
curl -s -X POST http://localhost:9090/classify \
  -H "Content-Type: application/json" \
  -d '{"task":"scaffold the config package"}' | jq .
```

The Aspire Dashboard will show:
- **Traces** tab: one span per `/classify` and `/implement` request
- **Metrics** tab: `gen_ai.client.token.usage` and `gen_ai.client.operation.duration` histograms
- **Structured logs** tab: gateway startup and shutdown events

## Metrics reference

| Metric | Type | Unit | Description |
|---|---|---|---|
| `gen_ai.client.token.usage` | Histogram | `{token}` | Tokens per completion by type |
| `gen_ai.client.operation.duration` | Histogram | `s` | Latency per completion |
| `gateway.tier.budget.remaining` | Gauge | `{token}` | Remaining daily budget per remote tier |

### Attributes on all metrics

| Attribute | Values | Description |
|---|---|---|
| `gen_ai.system` | `llama_cpp`, `deepseek`, `z_ai` | Provider |
| `gen_ai.request.model` | model alias | Model name |
| `gen_ai.token.type` | `input`, `output` | Token category |
| `gateway.tier` | `local_ornith`, `remote_deepseek`, … | Routing tier |
| `gateway.complexity` | `scaffold`, `single_file`, … | Task complexity |

## Production (Grafana)

Point the gateway at a Grafana OTLP endpoint:

```yaml
telemetry:
  enabled: true
  otlp:
    endpoint: https://your-grafana-otlp-endpoint
    insecure: false
  metrics:
    export_interval_sec: 60
  traces:
    sampling_ratio: 0.1   # 10% sampling in production
```

Set `OTEL_SEMCONV_STABILITY_OPT_IN=gen_ai_latest_experimental` if your
Grafana version supports the latest experimental GenAI conventions.

## Note on semconv stability

As of July 2026, the GenAI semantic conventions remain in Development
status. Attribute names are centralised in `internal/telemetry/attributes.go`
— when the spec stabilises, that is the only file to update.
