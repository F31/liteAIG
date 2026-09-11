# Core Benchmark Profile

Blocking target: Core Pipeline P99 <= 6ms, excluding provider network, external guardrails, semantic-cache embedding, and remote stream guards.

Reference release environment: Linux/amd64, Go 1.24.x, four dedicated vCPU, 8 GiB RAM, 30-second warm-up for load tests, and at least 100,000 measured end-to-end requests. The unit-level gate in `core_test.go` uses 20,000 post-warm-up samples and is intended to catch local orchestration regressions on every pull request.

Run:

```bash
go test ./tests/benchmark -run TestCorePipelineP99Budget -count=1 -v
go test ./tests/benchmark -bench BenchmarkCorePipeline -benchmem
```
