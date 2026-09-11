## ADDED Requirements

### Requirement: Phase 0 Fast Guard
Admission SHALL enforce body-size limits before expensive processing. Input Guardrail and non-stream Output Guardrail SHALL support deterministic keyword, regular-expression, and secret rules with allow, block, or redact actions. Cross-chunk stream detection and external classifiers are out of Phase 0 scope.

#### Scenario: Oversized request
- **WHEN** a request exceeds the configured body-size limit
- **THEN** Admission rejects it before content classifiers or connectors execute

#### Scenario: Blocked secret pattern
- **WHEN** an enabled deterministic rule detects a configured secret pattern in input
- **THEN** the request is blocked before upstream invocation
- **AND** a redacted guardrail event identifies the policy and rule versions

### Requirement: Simple request rate limiting
The gateway SHALL enforce a tenant- and project-scoped simple RPM policy before route resolution. A denied request MUST NOT reserve provider capacity or invoke an upstream connector.

#### Scenario: RPM exhausted
- **WHEN** the effective RPM limit has no remaining capacity
- **THEN** Preflight returns a rate-limit response with retry guidance
- **AND** Accounting records the denial without provider usage

### Requirement: Single-window budget
Phase 0 SHALL enforce one active hard or soft usage window at Tenant, Project, or Key scope using an idempotent reservation and reconciliation identifier. Hard exhaustion SHALL block before Resolution; soft exhaustion SHALL continue with an auditable warning.

#### Scenario: Hard budget exhausted
- **WHEN** estimated request usage would exceed an effective hard budget
- **THEN** Preflight rejects the request before route resolution and upstream invocation

#### Scenario: Actual usage differs from estimate
- **WHEN** a completed request consumes a different amount than reserved
- **THEN** finalization reconciles the reservation exactly once to actual measured usage

### Requirement: Unconditional exactly-once finalization
Accounting and Telemetry SHALL execute through top-level finalization for success, policy denial, guardrail block, provider failure, timeout, cancellation, and panic-safe error conversion. Repeated finalizer calls with the same request identifier SHALL NOT duplicate usage or budget reconciliation.

#### Scenario: Provider failure
- **WHEN** every provider attempt fails
- **THEN** the reservation is reconciled or released
- **AND** exactly one final request record is persisted with all attempt outcomes

#### Scenario: Duplicate finalization signal
- **WHEN** cancellation and stream termination race to finalize the same request
- **THEN** ledger and request facts are committed once

### Requirement: Request and usage facts
For every request, the system SHALL record request ID, timestamps, outcome, Tenant, Project, API Key, trusted Principal attribution, Logical Model, selected deployment, snapshot and policy versions, token usage, latency, guardrail outcome, retry and fallback counts, and available provider cost. Prompt and response bodies SHALL NOT be retained by default.

#### Scenario: Successful request accounting
- **WHEN** an upstream response reports token usage
- **THEN** the request record and usage fact contain matching normalized token counts
- **AND** Console views use server-side facts rather than browser recomputation

#### Scenario: Static Phase 0 pricing unavailable
- **WHEN** no approved static provider price is configured
- **THEN** the request displays cost as unavailable rather than zero or fabricated
- **AND** token usage remains accounted

### Requirement: Standard event contract
Business modules SHALL emit standard domain events through the Kernel event contract and MUST NOT import telemetry exporters or analytics storage implementations.

#### Scenario: Accounting completion event
- **WHEN** a request finalizes
- **THEN** a redacted completion event is emitted with correlation and scope fields
- **AND** exporter failure does not alter the already determined client response
