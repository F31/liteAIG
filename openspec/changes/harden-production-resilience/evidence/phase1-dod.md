# Phase 1 Definition of Done Evidence

Status: Complete

This document records the release-readiness evidence for the
`harden-production-resilience` change. The uninterrupted dedicated Redis soak
and all release gates completed successfully.

## Release Gates

| Gate | Result | Evidence |
| --- | --- | --- |
| Architecture boundaries | PASS | `go run ./cmd/architecture-test` |
| Go unit, repository, contract, isolation, and race suites | PASS | `LITEAIG_TEST_POSTGRES_DSN='postgres://liteaig:liteaig_test@localhost:55432/liteaig_test?sslmode=disable' LITEAIG_TEST_REDIS_ADDR='localhost:56379' go test -race -timeout 180s ./...` |
| Go static analysis | PASS | `go vet ./...` |
| Golden Scenario B | PASS | `tests/golden/scenario_b_test.go`, including 429, timeout, 5xx, slow TTFT, credential exhaustion, circuit-open, and explainable fallback evidence |
| Chaos scenarios | PASS | `tests/chaos/chaos_test.go`, including Redis hard fail-closed/soft fail-open and PostgreSQL outage snapshot survival |
| Live Tail privacy | PASS | `internal/controlplane/adminapi/server_test.go:TestLiveTailPayloadContainsSummaryOnly` verifies summary-only SSE payloads exclude prompt, response body, and authorization data |
| Tenant switch cancellation | PASS | `web/console/src/query.test.ts` verifies scoped request cancellation |
| Console quality gate | PASS | `npm run check`, including formatting, locale parity, hardcoded-string checks, 6 Vitest tests, and production build |
| Dependency audit | PASS | `npm audit` reported 0 vulnerabilities for the Console and SDK contract workspace |
| SDK contract smoke | PASS | `tests/contract/sdk` contract smoke suite |
| Workflow lint | PASS | `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/ci.yml` |
| Secret scan | PASS | `gitleaks --config .gitleaks.toml` reported no leaks |
| Core pipeline latency | PASS | `TestCorePipelineP99Budget`: p99 48 ns over 20,000 samples; `BenchmarkCorePipeline`: 17.09 ns/op, 0 allocs/op |
| Accessibility | PASS | Lighthouse 13.4.1 reported accessibility score 0.96 for `/health`; report: `/tmp/opencode/lighthouse/phase1-health.json` |
| OpenSpec strict validation | PASS | `openspec validate --all --strict`: 23 passed, 0 failed on 2026-08-30 |

## Soak Evidence

### Fast gates

- In-memory smoke: PASS in 0.25 seconds under the race detector.
- Redis short gate: PASS in 2.08 seconds under the race detector.
- Isolated accelerated Redis soak: PASS after 10 minutes at a 1 ms interval.
- The accelerated run completed the final full-budget reservation and all four
  lease-capacity acquisitions, proving no sustained reservation or lease leak
  at the end of the run.

### Uninterrupted 24-hour run (dedicated instance)

- Start: 2026-08-29 18:18:32 +08:00
- Completion: 2026-08-30 18:18:32 +08:00
- Process ID at launch: `1165903`
- Redis address: `localhost:56381` (DEDICATED — no test touches this instance)
- Command: `LITEAIG_TEST_REDIS_ADDR=localhost:56381 LITEAIG_SOAK_DURATION=24h LITEAIG_SOAK_INTERVAL=100ms go test ./tests/soak -run TestPhase1SoakHarness -count=1 -timeout=25h -v`
- Log: `/tmp/opencode/phase1-soak-24h-dedicated.log`
- Result: PASS (`TestPhase1SoakHarness`, 86400.05 seconds; package 86400.112 seconds)

### Invalidated runs (shared-Redis contamination, not ledger leaks)

Two earlier 24-hour attempts failed with `reserve rejected` because the shared
test Redis (`localhost:56379`) was `FlushDB`'d by other tests running against
the same instance — the same mechanism as the very first 2-second gate. Those
runs are retained only as invalidated records:

- `/tmp/opencode/phase1-soak-24h.log` (failed at 320.14s after a short gate
  flushed its Redis)
- `/tmp/opencode/phase1-soak-24h-restarted.log` (failed at 15220.19s after the
  full `go test -race ./...` suite ran against `localhost:56379`)

These do not indicate a ledger leak: the isolated accelerated soak (10 minutes,
1 ms interval, ~600k iterations, dedicated `localhost:56380`) completed with
zero sustained reservation/lease leak. The soak listed above runs on a
dedicated instance that no test will access.

## Completion Checklist

- [x] Required architecture, correctness, race, security, benchmark, UI,
  accessibility, chaos, privacy, and Golden Scenario B gates passed.
- [x] OpenSpec strict validation passed before final evidence completion.
- [x] The uninterrupted 24-hour soak passed its final reservation and lease
  capacity assertions.
- [x] Replace the pending soak result with its final duration and PASS output.
- [x] Mark tasks 11.5 and 11.6 complete and run strict validation again.
