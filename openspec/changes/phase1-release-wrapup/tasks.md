## 1. Durable Accounting Spool

- [x] 1.1 Add the `Spool` contract in `internal/finops/accounting` and a stdlib append-only WAL implementation in `internal/platform/storage/spool` with segment rotation, per-record checksum, and a configurable fsync policy.
- [x] 1.2 Add a durable finalizer that appends each finalized `Facts` record to the WAL, marks it locally durable, enqueues an asynchronous flush, ingests to PostgreSQL idempotently (`event_id` + `ON CONFLICT(request_id) DO NOTHING`), and checkpoints/truncates committed segments; keep flush off the Provider TTFT path.
- [x] 1.3 Add spool warn/critical/hard thresholds and per-Tenant `hard_accounting` / `soft_accounting` fail modes with per-Tenant/shard disk quotas; `hard_accounting` rejects new billable requests at the hard threshold, `soft_accounting` continues with a Critical Alert.
- [x] 1.4 Add low-cardinality `accounting_spool_*` metrics and spool-pressure degradation observability/alerts alongside existing telemetry.
- [x] 1.5 Add repository contract tests proving append→replay idempotency (no duplicate request/usage rows), checkpoint/truncate correctness, and budget reconcile/release independent of ingest completion.
- [x] 1.6 Add a chaos test: with the Accounting store down, finalized usage persists in the WAL and replays to zero duplicate rows after recovery.

## 2. Graceful Drain Lifecycle

- [x] 2.1 Implement a `Lifecycle` state machine (`RUNNING → DRAINING → EXIT`) in `internal/app` with SIGTERM/SIGINT handling.
- [x] 2.2 Wire `cmd/liteaig` to actually start the composed planes and run the lifecycle; `readyz` reports not-ready while draining.
- [x] 2.3 Add an active long-stream registry and streaming-aware shutdown honoring `drain_timeout`, `stream_drain_timeout`, and `force_shutdown_timeout`; refuse new long streams once draining.
- [x] 2.4 Ensure drain finalizes in-flight usage/budget/lease (reconcile or release) before process exit.
- [x] 2.5 Add drain tests: in-flight requests complete and readyz flips, long streams complete within `stream_drain_timeout`, force-shutdown caps the phase, and leases/reservations are released on drained exit.
- [x] 2.6 Add configuration validation tests for drain timeouts (positive, ordered, explicit defaults).

## 3. Secret Provider and Cache

- [x] 3.1 Define the replaceable `internal/platform/secrets.Provider` contract (resolve `secret_ref` → material) and a default memory/env-backed implementation so Standard does not require KMS yet.
- [x] 3.2 Implement a `CachingProvider` with `secret_cache_ttl`, `secret_stale_grace`, and `rotation_overlap`; serve cached credentials within grace during provider outage, fail closed after grace, require the provider for new/first/rotation decryption, and raise outage alerts/metrics.
- [x] 3.3 Route connector credential resolution through the provider by `secret_ref` so raw secrets never enter the request path or runtime snapshot.
- [x] 3.4 Add secret-leak regression tests: no plaintext in logs, telemetry, diffs, audit, or admin API responses; cross-tenant `secret_ref` resolution returns a non-disclosing error; no secret material persisted to disk unless envelope-encrypted and separate from RuntimeBundle.
- [x] 3.5 Add configuration/extension contract tests for secret cache settings and provider pluggability.

## 4. Configuration and Architecture

- [x] 4.1 Extend the config compiler and `TenantRuntimeSnapshot`/`GlobalRuntime` with spool thresholds/mode, drain timeouts, and secret cache settings, with validation and explicit defaults.
- [x] 4.2 Add architecture manifest/table-owner entries and architecture CI rules for the new `internal/platform/storage/spool` and `internal/platform/secrets` packages.
- [x] 4.3 Add zh-CN/en-US locale parity and hardcoded-string checks for any new Console text surfaced on Health & Circuits / HA surfaces (degraded dependency, spool pressure, drain state).

## 5. Release Gates (Scenario G basic)

- [x] 5.1 Add Scenario G basic assertions for Spool replay equivalence, graceful drain smoke, and Secret outage fail-open/fail-closed behavior in the chaos/golden suites.
- [x] 5.2 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve all blocking failures.
- [x] 5.3 Run `openspec validate --all --strict` and record the Phase 1 Scenario G basic DoD evidence in `openspec/changes/phase1-release-wrapup/evidence/`.
