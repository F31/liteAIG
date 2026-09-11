# modular-runtime-foundation Specification

## Purpose
TBD - created by archiving change deliver-phase0-governed-multimodel-access. Update Purpose after archive.
## Requirements
### Requirement: Single-binary modular runtime
LiteAIG SHALL build as one `liteaig` binary by default and SHALL support `all`, `gateway`, and `control` runtime modes without introducing internal HTTP or gRPC calls between modules in `all` mode.

#### Scenario: Combined mode uses in-process boundaries
- **WHEN** LiteAIG starts with `mode=all`
- **THEN** Control Plane and Data Plane capabilities run in one process through Go contracts
- **AND** no loopback network service is required for their internal interaction

### Requirement: Fixed seven-stage pipeline
The Data Plane SHALL execute Admission, Input Guardrail, Policy and Cost Preflight, Resolution, Execution and Resilience, Output or Stream Guardrail, and Accounting and Telemetry in that compile-time-fixed order. Extensions MUST NOT add, remove, or reorder stages.

#### Scenario: Normal request follows the fixed order
- **WHEN** an admitted request requires an upstream invocation
- **THEN** the request traverses all seven stages in the specified order
- **AND** each stage records its outcome in the request decision evidence

#### Scenario: Cache-compatible short circuit preserves governance
- **WHEN** a future cache implementation resolves a response during preflight
- **THEN** Resolution and Execution may be skipped
- **AND** Output Guardrail and Accounting still execute

### Requirement: Immutable request snapshot
Each request SHALL capture one `TenantRuntimeSnapshot` pointer during Admission and SHALL use that snapshot for its complete lifetime, including streaming and finalization.

#### Scenario: Publication occurs during a request
- **WHEN** a new tenant snapshot is activated while a request is in flight
- **THEN** the in-flight request continues with its captured snapshot version
- **AND** a subsequent request uses the newly active version

#### Scenario: Control Plane becomes unavailable
- **WHEN** the Data Plane has a valid loaded snapshot and the Control Plane becomes unavailable
- **THEN** the Data Plane continues forwarding requests with the loaded snapshot
- **AND** exposes that configuration convergence is degraded

### Requirement: Enforced module and data ownership
Architecture CI SHALL reject forbidden imports, Data Plane dependencies on Control Plane persistence, protocol adapter dependencies on domain implementations, cross-module concrete repository dependencies, unscoped tenant repository methods, and migrations whose declared owner differs from the table-owner manifest.

#### Scenario: Forbidden protocol dependency
- **WHEN** an ingress protocol package imports a guardrail, routing, budget, or FinOps implementation
- **THEN** `architecture-test` fails with the violated boundary and import path

#### Scenario: Incorrect migration owner
- **WHEN** a migration modifies a table owned by another module without matching owner metadata
- **THEN** `architecture-test` fails before merge

### Requirement: Runtime overhead benchmark
The implementation SHALL provide reproducible stage benchmarks with documented hardware, workload, warm-up, and exclusions, and Phase 0 release gates SHALL fail when the approved Core overhead budget is exceeded.

#### Scenario: Benchmark regression
- **WHEN** the blocking benchmark exceeds its approved P99 Core overhead threshold
- **THEN** the Phase 0 release gate fails and reports the regressed stage

