# guardrail-policy-persistence Specification

## ADDED Requirements

### Requirement: Durable guardrail policy
Guardrail Policies SHALL be persisted per Tenant and SHALL survive process restarts. Fast Publish SHALL write the policy row and record an audit event containing the prior and new Security Epoch. The active policy SHALL compile into the Tenant Runtime Snapshot so Stage 2 and Stage 6 enforcement uses the snapshot-compiled rules.

#### Scenario: Policy survives restart
- **WHEN** a process restarts after a Fast Publish
- **THEN** the active policy is reloaded from storage for its Tenant
- **AND** the Security Epoch is preserved

#### Scenario: Publish is audited
- **WHEN** an operator publishes a Tighten or Loosen change
- **THEN** an audit event records the actor, change type, and prior/new Security Epoch
- **AND** the prior epoch is never rewritten

### Requirement: Runtime enforcement from snapshot
Stage 2 (input) and Stage 6 (output) guardrail enforcement SHALL evaluate the policy compiled into the captured `TenantRuntimeSnapshot`, so a Fast Publish becomes effective at runtime without a full configuration publication.

#### Scenario: Tighten effective at runtime
- **WHEN** a guardrail policy is tightened and the tenant snapshot re-activated
- **THEN** subsequent requests are evaluated against the tightened rules
- **AND** in-flight requests continue with their captured snapshot version

### Requirement: Security epoch convergence
The published Security Epoch SHALL be monotonically increasing and visible on the Health/Governance surface so operators can confirm convergence.

#### Scenario: Epoch visible
- **WHEN** an operator views the active guardrail policy
- **THEN** the current Security Epoch is displayed
