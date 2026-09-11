# federated-rbac-permissions Specification

## ADDED Requirements

### Requirement: Backend-enforced permission matrix
The system SHALL enforce a backend-authoritative role→permission matrix including `external_agent.review`, `external_agent.suspend`, `external_agent.trust.rotate`, `external_agent.trust.revoke`, `approval.decide`, `delegation.grant`, and `delegation.revoke`. The frontend SHALL be display-only and never authoritative.

#### Scenario: Review is tenant-admin only
- **WHEN** a non-tenant-admin attempts `external_agent.review`
- **THEN** the action is denied by the backend
- **AND** the frontend hides it but does not grant it

#### Scenario: Suspend is operator-allowed
- **WHEN** a tenant operator attempts `external_agent.suspend`
- **THEN** the action is allowed
- **AND** an audit event records the suspend

### Requirement: Federated trust actions are permission-gated
Trust rotation and revocation SHALL require the corresponding permission and SHALL be audited. Introducing a new external trust relationship SHALL require `external_agent.review` (Tenant Admin), while emergency suspension SHALL be available to Tenant Operator.

#### Scenario: Trust rotation audited
- **WHEN** an authorized operator rotates a trust anchor
- **THEN** the rotation is recorded in Audit
- **AND** the action requires `external_agent.trust.rotate`
