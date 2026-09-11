# delegation-agent-acl Specification

## ADDED Requirements

### Requirement: Delegation intersects and only shrinks privilege
An agent handoff SHALL compute effective permission as the intersection of the delegator's permission, the grant's permission, and the delegatee's policy. A handoff SHALL NEVER widen privilege, and remote self-claimed delegation from an external agent SHALL NOT be included.

#### Scenario: Delegation cannot widen
- **WHEN** agent A delegates to agent B
- **THEN** B's effective permission is A ∩ grant ∩ B's policy
- **AND** B never gains more than A had

#### Scenario: External self-claimed delegation ignored
- **WHEN** an external agent claims a delegation chain in its payload
- **THEN** the claim is not part of the local intersection
- **AND** only local provable grants count

### Requirement: Agent ACL gating
Delegation SHALL be gated by an Agent ACL enumerating which agents may receive a delegation. A handoff to an agent not in the ACL SHALL be denied before execution.

#### Scenario: Denied delegation target
- **WHEN** a handoff targets an agent not in the Agent ACL
- **THEN** the delegation is denied
- **AND** the denial is attributable
