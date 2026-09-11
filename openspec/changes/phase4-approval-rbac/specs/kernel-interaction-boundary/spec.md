# kernel-interaction-boundary Specification (Delta)

## ADDED Requirements

### Requirement: Approval interaction carries request and decision
The `approval` InteractionKind SHALL carry an approval request (action, target, requester, required approvers) and the pipeline context SHALL carry the decision state so a `REQUIRE_APPROVAL` checkpoint can block execution until SoD/dual-approval is satisfied. This is additive and does not add a new InteractionKind.

#### Scenario: Approval checkpoint blocks execution
- **WHEN** a request carries a pending approval interaction
- **THEN** the pipeline blocks before the connector
- **AND** the decision state is attributable to the request
