# Gateway Benchmark

`cmd/loadbench` drives a real LiteAIG Gateway with OpenAI-compatible chat requests and reports throughput, latency percentiles, streaming TTFT, error rate, and SLO violations.

## Command

```sh
go run ./cmd/loadbench \
  --gateway http://127.0.0.1:18082 \
  --key sk-lia-v1_... \
  --model default-chat \
  --duration 10s \
  --concurrency 8 \
  --rps-floor 50 \
  --p99-ms-floor 500 \
  --max-error-rate 0.02 \
  --json \
  --report bench-chat.json
```

Add `--stream` to measure SSE streaming and `ttftP99Ms`.

For benchmark-only local runs, raise the gateway limiter above the target load, for example `--gateway-rpm=60000 --gateway-burst=60000`. The Lite default is 600 RPM/burst and will intentionally produce `429 Too Many Requests` once exhausted.

## Local Reference

SQLite LiteAIG + loopback mock OpenAI provider, 5s duration, concurrency 8, limiter raised to 60000 RPM/burst:

| Mode | RPS | P99 | TTFT P99 | Error Rate |
|---|---:|---:|---:|---:|
| chat | 746.3 | 62.2 ms | n/a | 0% |
| stream | 1720.6 | 8.1 ms | 8.1 ms | 0% |

Treat these as a local smoke baseline, not a Standard/Enterprise capacity claim. Capture topology, provider behavior, limiter settings, duration, concurrency, and report artifact with every release benchmark.
