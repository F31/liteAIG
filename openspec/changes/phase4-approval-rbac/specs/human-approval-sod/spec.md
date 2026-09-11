# human-approval-sod Specification

## ADDED Requirements

### Requirement: Approval lifecycle with separation of duties
High-risk actions SHALL go through an approval request with a lifecycle `pending → approved → rejected → cancelled`. A requester MUST NOT approve their own request. When Enterprise dual approval is configured, at least two distinct approvers, neither the requester, SHALL approve. Every decision SHALL be recorded in Audit with actor and timestamp.

#### Scenario: Requester cannot self-approve
- **WHEN** the requester of an approval attempts to approve it
- **THEN** the decision is rejected
- **AND** an audit event records the denied attempt

#### Scenario: Dual approval requires two distinct approvers
- **WHEN** a dual-approval request has only one approver
- **THEN** it remains pending
- **AND** a second distinct approver (not the requester) is required before it becomes approved

### Requirement: Approval blocks execution
A `REQUIRE_APPROVAL` action SHALL block execution before the downstream connector until the approval decision satisfies SoD/dual-approval. The block SHALL be attributable to the request and the pending approval.

#### Scenario: Unapproved high-risk action blocked
- **WHEN** a task reaches a high-risk action awaiting approval
- **THEN** the action is blocked before invocation
- **AND** the block is attributable to the pending approval
