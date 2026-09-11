## ADDED Requirements

### Requirement: Drift detection
The system SHALL compare the active compiled runtime view against the published configuration version and report drift with a severity and a config_drift metric. Gateway readiness SHALL account for drift exceeding the configured grace period.

#### Scenario: Unexpected live divergence
- **WHEN** the live runtime differs from the published version beyond the grace window
- **THEN** drift is reported on Health and surfaced in the Console
- **AND** a config_drift metric is emitted

### Requirement: Drift reconciliation
Reconciliation SHALL re-compile and atomically re-activate the published configuration without creating a new version history entry, or SHALL report the reason it cannot reconcile. Ordinary config toggles MUST NOT bypass this path.

#### Scenario: Reconciliation restores snapshot
- **WHEN** reconciliation runs against a drifted runtime
- **THEN** the active snapshot matches the published version
- **AND** an audit event records the reconciliation

### Requirement: Gateway readiness coupling
Data Plane readiness SHALL require at least one active snapshot, config_drift within grace, and Security Epoch staleness within the configured bound for strict security mode.

#### Scenario: Not ready during drift
- **WHEN** drift exceeds the grace period
- **THEN** the gateway reports not-ready for load balancing
- **AND** still serves in-flight requests using the loaded snapshot
