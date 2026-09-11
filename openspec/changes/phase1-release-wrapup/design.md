# Phase 1 Release Wrap-up — Design

## Context

The `harden-production-resilience` change completed the coordination, isolation, circuit, drift, attribution, OTel, alert, and Scenario B work, but three V8.2 Phase 1 items were not covered by its task list:

1. **Durable Accounting Spool** — today `sqlrepo.AccountingRepository.Finalize` performs a synchronous PostgreSQL write (`request_records` + `usage_events` in one tx, `internal/platform/storage/sqlrepo/accounting.go:16`). When PostgreSQL is unavailable, `Finalize` returns an error and the provider-produced usage is silently dropped. V8.2 §3.7/§30.9/§2.8#6 requires at-least-once durability with idempotent replay.
2. **Graceful Drain** — `cmd/liteaig/main.go:14` only parses `-mode` and prints a plan; no server lifecycle, signal handling, readiness, or streaming protection exists. V8.2 §30.10/§2.8#8 requires drain-before-kill with SSE/A2A/MCP long-stream protection.
3. **Secret cache TTL/grace** — credentials are referenced by `SecretRef` in the snapshot (`internal/kernel/runtime/snapshot.go`), but there is no replaceable Secret Provider or cache; no TTL/stale-grace/rotation semantics. V8.2 §25.1.1 requires KMS not to be a synchronous hot-path dependency.

All three map to the basic Scenario G (Production HA / DR) gate and are prerequisites for Phase 2+ (durable FinOps, upgrade safety, commercial secret handling).

## Goals / Non-Goals

**Goals:**
- Provider-produced usage is durably captured on the local node before any asynchronous ingest, and replayed idempotently after an Accounting DB outage.
- A SIGTERM-driven drain lifecycle stops new traffic, finishes in-flight requests, protects long streams, and finalizes usage/budget/lease before exit.
- A replaceable Secret Provider caches credentials with TTL/stale-grace/rotation semantics so provider outage degrades predictably (fail-open within grace, fail-closed after grace).
- All thresholds, timeouts, and modes are validated typed configuration (per extensibility principle), not scattered constants.

**Non-Goals:**
- ClickHouse/Kafka-compatible analytics sink, real KMS/Vault integration, multi-currency/chargeback, Semantic Cache, OIDC/SAML/SCIM, and full Region DR.
- Any change to the fixed seven-stage pipeline order; Accounting and Telemetry remain Stage 7.
- Agent Version Drain (`draft → active → draining → retired`) — that is Phase 4 Agent Versioning, not node-level drain.

## Decisions

### 1. Spool is a local append-only WAL owned by Platform, consumed by FinOps
Place the durable store under `internal/platform/storage/spool` with a `Spool` contract declared in `internal/finops/accounting` (mirroring how `Repository` is a FinOps contract with a Platform implementation). Records are serialized `accounting.Facts` with `event_id`, `request_id`, `tenant_id`, `event_ts`, usage, cost, pricing version, and a checksum, appended to rotated segment files at `/data/spool/accounting/`.

Rationale: Spool is a local recovery mechanism (V8.2 §30.9), not a long-term analytics store; keeping it Platform-owned avoids a second FinOps table owner and lets the node persist before any network dependency.
Alternative considered: PostgreSQL as the sole accounting authority. Rejected — it is exactly the single point of failure this change removes, and conflicts with V8.2 §2.8#6.

### 2. Finalize becomes append → fsync → async ingest, off the TTFT hot path
Stage 7 (`internal/gateway/accounting/handler.go`) wraps the existing `Finalizer` with a durable adapter: append to the WAL with a configurable fsync policy, mark locally durable, enqueue an async flush, ingest to PostgreSQL, then checkpoint/truncate the committed segment. The flush goroutine retries with backoff on DB failure. Ingestion is idempotent using the existing `ON CONFLICT(request_id) DO NOTHING` (request_records) plus `event_id` dedupe for usage_events.

Rationale: preserves the at-least-once + idempotent model V8.2 §2.8#11 mandates, and keeps Provider TTFT free of Spool flush work.
Alternative considered: synchronous ingest with retry only. Rejected — retry without a durable spool still drops usage on process kill.

### 3. Thresholds and fail modes are per-Tenant typed configuration
`spool.warn_threshold` (70%), `spool.critical_threshold` (90%), `spool.hard_threshold` (100%) and per-Tenant `hard_accounting` / `soft_accounting` are compiled into the Tenant Runtime Snapshot. `hard_accounting` rejects new billable requests when the Spool cannot guarantee persistence; `soft_accounting` continues with a Critical Alert and an exposed unsettled-risk signal. Per-Tenant/shard quotas bound disk usage.

Rationale: matches the existing Budget hard/soft fail-mode pattern (`internal/finops/budget/failmode.go`) and satisfies dependency-specific degradation (§2.8#5).

### 4. Drain is a runtime lifecycle state machine in `internal/app`
Add a `Lifecycle` with states `RUNNING → DRAINING → EXIT`. On SIGTERM/SIGINT: set `DRAINING`, flip `readyz` to not-ready so load balancers stop new traffic, let in-flight requests finish within `drain_timeout`, protect registered long streams (SSE Live Tail and future A2A/MCP) up to `stream_drain_timeout`, then cancel remaining upstream, finalize usage/budget/lease, and exit within `force_shutdown_timeout`. `cmd/liteaig/main.go` starts the composed planes and runs the lifecycle; a registry tracks active streams.

Rationale: readiness must not claim ready while draining (V8.2 §2.3.4 `process not draining`), and streaming-aware upgrade requires drain before termination (§2.8#8, §30.10).
Alternative considered: relying on Kubernetes termination grace only. Rejected — the process must drive its own drain to reconcile budget/lease/usage.

### 5. Secret Provider is a replaceable contract with a caching wrapper
Define `internal/platform/secrets.Provider` (resolve `secret_ref` → material; decrypt). A `CachingProvider` wraps any Provider with an in-memory cache governed by `secret_cache_ttl`, `secret_stale_grace`, and `rotation_overlap`. Credentials already decrypted and within TTL/grace continue during an outage; new decryption, first use, and rotation always require the Provider; after `stale_grace` the credential policy fails closed. Provider outage is observable (metrics/alert). A memory/env-backed Provider ships as the default so Standard does not require KMS yet; KMS/Vault is a Phase 4 Integrate.

Rationale: satisfies §25.1.1 (KMS not on the request hot path, fail-open within grace, fail-closed after grace, no unlimited stale use) while keeping the abstraction replaceable.

### 6. Configuration is validated and compiled
New settings (spool thresholds/mode, drain timeouts, secret cache TTL/grace/rotation) enter the config compiler and Tenant Runtime Snapshot/GlobalRuntime with explicit defaults and validation, so behavior is not hardcoded and existing deployments keep working with defaults.

## Risks / Trade-offs

- [Spool disk fills under sustained DB outage] → hard/critical thresholds, per-Tenant/shard quotas, hard_accounting rejection, Critical Alert.
- [fsync adds latency to finalization] → flush is off the TTFT path; fsync policy configurable (per-segment vs. group commit); budget/lease finalization continues even if spool ingest lags.
- [Drain kills long streams abruptly] → stream_drain_timeout + force_shutdown_timeout; active-stream registry; readiness flips before wait so LB stops new streams.
- [Secret cache serves stale credentials] → stale_grace bounds it; fail-closed after grace; outage alerts; rotation requires the provider.
- [Replay duplicates after restart] → idempotent ingest via request_id conflict + event_id dedupe; chaos replay-equivalence test asserts zero duplicate usage rows.

## Migration Plan

1. Land the Spool contract + WAL implementation and a `DurableFinalizer` behind the existing `Finalizer` shape; default flush is immediate ingest with spool fallback (no behavior change when DB is healthy).
2. Add drain lifecycle and wire `cmd/liteaig` to start planes and run it; readiness endpoint reflects drain state.
3. Add Secret Provider + caching wrapper and switch credential resolution through it.
4. Extend config compiler/snapshot with the new settings; extend architecture manifest/table-owner entries for new packages.
5. Add chaos/repository/golden tests; verify via `go test -race ./...`, `npm run check`, `openspec validate --all --strict`, and the Scenario G basic assertions.
6. Rollback: each capability is behind its config defaults; disabling spool restores direct synchronous ingest, drain defaults to a short force-shutdown, and a passthrough Provider disables caching.

## Open Questions

- Should the accounting spool use a third-party WAL (e.g., bbolt) or stdlib append-only files? Default decision: stdlib append-only files with checksums to avoid a new dependency; revisit if a larger node-local store is needed in Phase 3.
- Should `readyz` include Spool health for `hard_accounting` tenants? Default: no — Spool is a recovery mechanism and must not make all Gateways simultaneously not-ready; only rejection of new billable calls for hard-accounting tenants.
