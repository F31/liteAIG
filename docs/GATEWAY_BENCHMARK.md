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

## Standard-Tier Reference (real topology)

Standard topology: two `mode=all` replicas sharing **Postgres + Redis coordination** (`--coordinator=redis://...`), `--gateway-rpm=60000 --gateway-burst=60000`, loopback mock provider, 20s windows, concurrency 8 per replica. Load split by hitting each replica's gateway directly. Measured on `beececd5` (Linux/amd64, containerized Postgres 17 / Redis 7 on localhost):

| Mode | Topology | RPS (per replica) | Combined RPS | P99 | TTFT P99 | Error Rate |
|---|---|---:|---:|---:|---:|---:|
| chat | single replica | 669 / 631 | — | 16.5 ms | n/a | 0% |
| chat | two replicas (split) | 609 / 612 | **1221** | 25.2 ms | n/a | 0% |
| stream | single replica | 482 | — | 22.2 ms | 22.0 ms | 0% |
| stream | two replicas (split) | 501 / 648 | **1149** | 25.4 ms | 24.9 ms | 0% |

Pod-replacement resilience: killing one replica left the surviving replica serving uninterrupted on the shared configuration/key (all state lives in Postgres + Redis); no data-plane interruption was observed.

Standard-tier throughput is lower than the SQLite local smoke because every request persists accounting to Postgres and coordinates via Redis; the Standard numbers above are the production-representative baseline for capacity planning.
