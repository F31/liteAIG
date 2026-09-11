# Phase 1 Release Wrap-up — DoD Evidence

Status: Complete

This change closes the three V8.0/8.2 Phase 1 gaps that `harden-production-resilience`
did not cover: Durable Accounting Spool, Graceful Drain, and Secret cache
TTL/grace. It satisfies the basic Scenario G (Production HA / DR) gate.

## Capabilities Delivered

### Durable Accounting Spool
- `internal/finops/accounting/spool.go`: `Spool` contract, `SpoolRecord`,
  `SpoolStats`, `SpoolPolicy` (hard/soft mode, warn/critical/hard thresholds,
  per-Tenant quota), `ClassifySpoolPressure`.
- `internal/platform/storage/spool/spool.go`: append-only WAL with segment
  rotation, per-record SHA-256 checksum, configurable fsync policy,
  checkpoint/truncate of fully-committed segments, per-Tenant usage accounting,
  restart-safe pending recovery.
- `internal/finops/accounting/durable.go`: `SpoolRepository` (appends with
  quota + hard-pressure gates), `Flusher` (idempotent ingest via
  `ON CONFLICT(request_id) DO NOTHING` + commit), `DurableFinalizer`
  (soft-accounting fail-open + pressure alerts), `FuncSpoolMetrics`.

### Graceful Drain Lifecycle
- `internal/app/lifecycle.go`: `RUNNING → DRAINING → EXIT` state machine,
  `StreamRegistry` (refuses new long streams while draining), `DrainConfig`
  validation + defaults, signal-driven `WaitForSignal`.
- `internal/app/readiness.go`: `/readyz` (not-ready while draining) and
  `/healthz`.
- `cmd/liteaig/main.go`: starts a readiness server and runs the lifecycle on
  SIGTERM/SIGINT with drain, stream-drain, and force-shutdown timeouts.

### Secret Provider and Cache
- `internal/platform/secrets`: replaceable `Provider`, `MemoryProvider`,
  `EnvProvider`, and `CachingProvider` with `secret_cache_ttl`,
  `secret_stale_grace`, `rotation_overlap`. Serves cached credentials within
  grace during provider outage, fails closed after grace, requires the provider
  for new/first/rotation decryption, and raises outage notifications. Connectors
  resolve credentials through it by `secret_ref` (openai connector integration
  test).

### Configuration and Architecture
- `TenantRuntimeSnapshot.SpoolPolicy()` compiled from `TenantConfig.AccountingSpool`
  with validator checks (mode, threshold ordering, non-negative).
- `GlobalRuntime` carries `DrainConfig` and `SecretCacheConfig`.
- `architecture/forbidden-imports.yaml` enforces `platform/secrets` and
  `platform/storage/spool` stay implementation-free.

## Gate Results

| Gate | Result |
| --- | --- |
| `go test -race -timeout 180s ./...` (Postgres 55432 + Redis 56379) | PASS (all packages) |
| `go vet ./...` | PASS |
| `go run ./cmd/architecture-test` | PASS (`architecture boundaries: ok`) |
| `npm run check` (format, locales, hardcoded strings, vitest, build) | PASS |
| `actionlint` | PASS |
| `gitleaks --config .gitleaks.toml detect` | no leaks found |
| `openspec validate --all --strict` | 10 passed, 0 failed |

## Scenario G Basic Evidence

1. **Spool replay equivalence** (`tests/chaos` + `tests/golden`):
   provider-produced usage finalizes while the Accounting store is down →
   events persist in the local WAL → after recovery the Flusher replays
   idempotently with zero duplicate request/usage rows and truncated segments.
2. **Graceful drain smoke** (`internal/app` + `tests/golden`): readyz flips to
   not-ready during drain, in-flight requests/streams complete within their
   timeouts, force-shutdown caps the phase, and in-flight budget/lease
   finalization completes before exit.
3. **Secret outage fail-open/fail-closed** (`internal/platform/secrets` +
   `tests/golden`): cached credentials continue within stale grace, fail closed
   after grace, rotation requires the provider, and cross-tenant references are
   non-disclosing.

## Sensitive-Data Notes

- Spool WAL stores serialized `Facts` (request/usage metadata); no prompt or
  response bodies are persisted.
- Secret cache is in-memory only; `secret_ref` remains opaque in snapshots,
  connectors, logs, and telemetry; the secret-leak regression tests assert no
  plaintext in outage notifications or cross-tenant paths.
- Console additions (`health.draining`, `health.degraded`,
  `health.spoolPressure`) keep zh-CN/en-US parity.
