# dr-runbook-checklist Specification

## ADDED Requirements

### Requirement: Deterministic DR runbook
The system SHALL provide a deterministic DR Runbook checklist with ordered steps (validate runtime, reconcile accounting, reconcile budget, switch traffic, verify readiness). Each step SHALL produce a pass/fail status and RTO/RPO hints. A failed preflight SHALL halt the runbook (fail-fast).

#### Scenario: Runbook executes in order
- **WHEN** a DR drill runs the checklist
- **THEN** steps execute in the fixed order
- **AND** a failed step halts the runbook with the failing step recorded

#### Scenario: Steps are attributable
- **WHEN** a step completes
- **THEN** its status and RTO/RPO hints are recorded
- **AND** the runbook is read-only (drill validation, not live failover)