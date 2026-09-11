# external-procurement-finops Specification

## ADDED Requirements

### Requirement: External agent procurement cost
External Agent invocations SHALL record `external_agent_cost` as a distinct procurement dimension alongside the task total. Task total SHALL include external agent cost; finance SHALL be able to view External Procurement separately (per relationship/vendor).

#### Scenario: Procurement cost is separate
- **WHEN** a task invokes an external agent
- **THEN** the external agent cost is part of the task total
- **AND** it is also visible under External Procurement per relationship

### Requirement: Dual budget cannot bypass task total
An External Procurement Budget SHALL bound external agent spend per relationship/vendor. Enforcing the procurement budget MUST NOT relax the Task Total Budget: both budgets SHALL pass independently, and the procurement budget MUST NOT be usable to bypass a task-level cost limit.

#### Scenario: Procurement budget exhausted does not relax task total
- **WHEN** a task would exceed its Task Total Budget but the procurement budget has headroom
- **THEN** the task is still rejected by the Task Total Budget
- **AND** when the procurement budget is exhausted the task is rejected by it as well
