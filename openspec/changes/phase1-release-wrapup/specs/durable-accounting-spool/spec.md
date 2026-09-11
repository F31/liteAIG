# durable-accounting-spool Specification

## ADDED Requirements

### Requirement: Durable local spool write
Stage 7 Accounting SHALL durably append every finalized provider-produced usage record to a local append-only WAL before any asynchronous ingest. Each record SHALL carry `event_id`, `request_id`, `tenant_id`, `event_ts`, usage, cost, pricing version, and a checksum. Append SHALL use a configurable fsync policy, and the flush to PostgreSQL/Event Sink SHALL NOT execute on the Provider TTFT hot path.

#### Scenario: Usage persists during Accounting DB outage
- **WHEN** a billable request finalizes while PostgreSQL is unavailable
- **THEN** the usage record is appended and fsync-acknowledged on the local WAL
- **AND** the request reports success without dropping the provider-produced usage

#### Scenario: Spool flush stays off the hot path
- **WHEN** a request is in the Execution and Resilience stage
- **THEN** no Spool flush or ingest work blocks the provider response path

### Requirement: Idempotent ingest and replay
The Spool SHALL ingest each event to the Accounting store at-least-once and MUST NOT double-count. Replay SHALL dedupe on `event_id` and request identity so a replayed record inserts no duplicate request or usage rows. A checkpoint SHALL truncate only segments whose events are acknowledged as ingested.

#### Scenario: Replay after restart does not duplicate
- **WHEN** the Accounting DB recovers and a spooled event is ingested again
- **THEN** the request record and usage event are inserted exactly once
- **AND** the committed segment is checkpointed and truncated

### Requirement: Thresholds and accounting fail modes
The Spool SHALL expose warn, critical, and hard thresholds with per-Tenant `hard_accounting` or `soft_accounting` modes compiled from the Tenant Runtime Snapshot. `hard_accounting` SHALL reject new billable requests when the Spool cannot guarantee persistence; `soft_accounting` SHALL continue serving while emitting a Critical Alert and exposing the unsettled-risk signal.

#### Scenario: Hard accounting rejects on spool pressure
- **WHEN** a `hard_accounting` Tenant's Spool reaches its hard threshold
- **THEN** new billable requests for that Tenant are rejected before execution
- **AND** a Critical Alert records the spool-pressure event

#### Scenario: Soft accounting continues with alert
- **WHEN** a `soft_accounting` Tenant's Spool reaches its critical threshold
- **THEN** new billable requests continue
- **AND** a Critical Alert exposes the unsettled accounting risk

### Requirement: Bounded disk usage with tenant quotas
The Spool SHALL bound total disk usage and SHALL prevent any single Tenant from consuming the whole node by enforcing per-Tenant or per-shard quotas.

#### Scenario: Tenant quota caps spool growth
- **WHEN** one Tenant's spooled events exceed its configured quota
- **THEN** that Tenant's new spool appends are constrained or rejected
- **AND** other Tenants' appends remain available

## MODIFIED Requirements

### Requirement: Finalization writes through the durable spool
The existing Stage 7 Accounting finalizer SHALL write finalized usage through the durable Spool path described above, preserving the existing idempotent PostgreSQL insert behavior (`ON CONFLICT(request_id) DO NOTHING`) as the ingest step. Budget reconcile/release and telemetry emission SHALL continue regardless of whether the asynchronous ingest has completed.

#### Scenario: DB healthy, spool is a pass-through
- **WHEN** PostgreSQL is healthy and the request finalizes
- **THEN** usage is appended to the WAL, ingested immediately, and the request record appears in the Accounting store
- **AND** no caller-visible behavior differs from direct synchronous write

#### Scenario: Budget finalization independent of ingest
- **WHEN** a request finalizes but the Accounting ingest is delayed by DB unavailability
- **THEN** budget reconcile/release completes normally
- **AND** the usage remains queued in the WAL for later replay
