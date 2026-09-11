# Phase 1 Release Wrap-up

## Why

The `harden-production-resilience` change delivered Phase 1 coordination, isolation, circuit, drift, attribution, OTel, alerts, and Scenario B, but three V8.2 Phase 1 requirements remain unimplemented: Durable Accounting Spool (Scenario G "Accounting DB outage 不丢账"), Graceful Drain / streaming-aware shutdown (Scenario G "rolling upgrade 先 Drain"), and Secret cache TTL/grace (Scenario G "Secret Cache 只在 TTL/grace 内继续"). Without these, Phase 1 cannot claim the basic Scenario G HA gate, and Phase 2+ cannot build on durable accounting or a correct drain lifecycle.

## What Changes

- Add a Durable Accounting Spool behind the existing Stage 7 Accounting path: local WAL segments with fsync policy, idempotent ingest into PostgreSQL with `event_id` dedupe, checkpoint/truncate, warn/critical/hard thresholds, per-Tenant/shard quotas, and `hard_accounting` / `soft_accounting` fail modes. Provider-produced usage MUST NOT be dropped when PostgreSQL is unavailable; Spool flush stays off the Provider TTFT hot path.
- Add a Graceful Drain lifecycle to the runtime: on SIGTERM the process enters `DRAINING`, `readyz` reports not-ready, LB stops new traffic, in-flight requests complete, SSE/A2A/MCP long streams are protected with `stream_drain_timeout`, and usage/budget/lease are finalized before exit. Configurable `drain_timeout` / `stream_drain_timeout` / `force_shutdown_timeout`.
- Add a replaceable Secret Provider abstraction with an in-memory credential cache: `secret_cache_ttl`, `secret_stale_grace`, `rotation_overlap`. Credentials already decrypted and within TTL/grace continue during a Secret Provider outage; new decryption, first use, and rotation still require the provider; after grace the credential policy fails closed; outage is observable and alerts.
- Wire the real runtime entrypoint in `cmd/liteaig` to actually start the composed planes with signal-driven graceful shutdown, and surface `readyz`/drain state and Spool health on the operations surfaces.

## Capabilities

### New Capabilities
- `durable-accounting-spool`: Durable at-least-once accounting with local WAL, idempotent ingest, thresholds, quotas, and hard/soft accounting fail modes.
- `graceful-drain-lifecycle`: Signal-driven drain state machine, readiness coupling, streaming-aware shutdown, and finalized accounting/budget/lease on exit.
- `secret-cache-resilience`: Replaceable Secret Provider with TTL/grace/rotation-overlap credential caching and fail-open-within-grace / fail-closed-after-grace degradation.

### Modified Capabilities
- `modular-runtime-foundation`: Extends the single-binary runtime requirement with the drain lifecycle and readiness contract so that draining processes stop accepting new traffic while in-flight and streaming requests complete.

## Impact

- **Backend**: new Spool storage and flush/ingest under `internal/finops/accounting` (WAL, checkpoint, quotas); new runtime lifecycle wiring in `internal/app` and `cmd/liteaig` (signal handling, drain state, readyz); new `internal/platform/secrets` Secret Provider interface and cache; integration in `internal/gateway/accounting/handler.go` (Stage 7) and the execution/streaming paths; new low-cardinality metrics `accounting_spool_*` and `gateway_drain_state` in `internal/observability`.
- **APIs**: readiness endpoint reflecting drain state; Spool and Secret degradation surfaced on Health & Circuits / HA surfaces (backend-authoritative, localized Console labels).
- **Dependencies**: no new third-party runtime dependencies (stdlib WAL + existing event sink/PostgreSQL repository).
- **Tests**: chaos additions for Spool replay equivalence, drain smoke, and Secret outage fail-open/fail-closed; repository contract tests for idempotent ingest; architecture checks for the new package boundaries.

### Non-Goals
- ClickHouse / Kafka-compatible analytics sink, real KMS/Vault integration, and multi-currency/chargeback remain out of scope (Phase 3/4 Integrate).
- Semantic Cache, OIDC/SAML/SCIM, and full Region DR are not delivered here.
- This change does not alter the fixed seven-stage pipeline order; Accounting and Telemetry remain Stage 7.

**Golden Scenario:** maps to Scenario G (Production HA / DR) basic coverage — Spool replay after Accounting DB outage, graceful drain during rolling upgrade, and Secret cache TTL/grace during KMS outage.
