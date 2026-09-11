## ADDED Requirements

### Requirement: Credential pool selection
Credential Pools SHALL select credentials using round-robin, weighted, least-inflight, or quota-aware strategies as configured. Selection MUST only consider credentials that are enabled, healthy, and not circuit-open.

#### Scenario: Weighted pool selection
- **WHEN** a pool is configured with weighted credentials
- **THEN** selection honors the configured weights over eligible credentials

#### Scenario: Ineligible credential excluded
- **WHEN** a credential is disabled, unhealthy, or circuit-open
- **THEN** pool selection skips it without using it for a request

### Requirement: Credential rotation on 429
When the selected credential returns a retryable 429, execution SHALL rotate to the next eligible credential in the pool when available, within the total call budget. Each attempt SHALL record the credential used for cost and circuit attribution.

#### Scenario: 429 rotates credential
- **WHEN** the active credential returns 429 and rotation budget remains
- **THEN** the next eligible credential is used
- **AND** both attempts are recorded with their credential identities

### Requirement: Per-credential circuit, health, and cost
Each credential SHALL maintain independent circuit, health, and cost accounting. Exhaustion or failure of one credential MUST NOT silently fail the pool while another eligible credential exists.

#### Scenario: Credential-specific circuit
- **WHEN** one credential's circuit opens
- **THEN** only that credential is excluded
- **AND** pool selection continues with other eligible credentials

### Requirement: Pool-aware compilation
Credential Pool membership and strategy SHALL be compiled into the Tenant Runtime Snapshot and resolved at execution time without per-request repository access.

#### Scenario: Snapshot-driven pool
- **WHEN** a request selects a deployment backed by a pool
- **THEN** pool membership and strategy come from the captured snapshot
