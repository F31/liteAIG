## MODIFIED Requirements

### Requirement: Simple request rate limiting
The gateway SHALL enforce a tenant- and project-scoped simple RPM policy before route resolution. A denied request MUST NOT reserve provider capacity or invoke an upstream connector. Phase 1 SHALL additionally support User and OrgUnit RPM dimensions when trusted identity attribution is present.

#### Scenario: RPM exhausted
- **WHEN** the effective RPM limit has no remaining capacity
- **THEN** Preflight returns a rate-limit response with retry guidance
- **AND** Accounting records the denial without provider usage

#### Scenario: OrgUnit RPM
- **WHEN** a trusted user's OrgUnit exceeds its configured RPM
- **THEN** the request is denied before route resolution
- **AND** requests without trusted identity are not subject to OrgUnit RPM

### Requirement: Single-window budget
Phase 0 SHALL enforce one active hard or soft usage window at Tenant, Project, or Key scope using an idempotent reservation and reconciliation identifier. Hard exhaustion SHALL block before Resolution; soft exhaustion SHALL continue with an auditable warning. Phase 1 SHALL extend Standard/Enterprise to the distributed multi-window atomic reservation ledger specified by `distributed-budget-ledger`, which MAY add User and OrgUnit windows when trusted attribution exists.

#### Scenario: Hard budget exhausted
- **WHEN** estimated request usage would exceed an effective hard budget
- **THEN** Preflight rejects the request before route resolution and upstream invocation

#### Scenario: Actual usage differs from estimate
- **WHEN** a completed request consumes a different amount than reserved
- **THEN** finalization reconciles the reservation exactly once to actual measured usage

#### Scenario: Multi-window atomic rejection
- **WHEN** a Phase 1 request would exceed any active Tenant, Project, or Key window
- **THEN** the atomic ledger rejects the reservation
- **AND** no window counter is modified
