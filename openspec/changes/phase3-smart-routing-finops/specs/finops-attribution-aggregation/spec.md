# finops-attribution-aggregation Specification

## ADDED Requirements

### Requirement: Multi-dimensional usage aggregation
The system SHALL aggregate Usage facts across Tenant, Project, OrgUnit, User, Application, Agent, and Task scopes. Aggregations SHALL be derived from the Usage Ledger so Dashboard totals and ledger SQL reconcile to the same numbers. The browser SHALL receive only aggregated results, never accumulate raw usage detail.

#### Scenario: Project x Department cross analysis
- **WHEN** an operator queries usage by Project and Department
- **THEN** the aggregation matches the Usage Ledger for the same scope
- **AND** the trust level of each attribution is displayed

#### Scenario: Browser receives aggregates only
- **WHEN** the Console requests usage
- **THEN** the response contains aggregated rows, not raw per-request usage that the browser accumulates

### Requirement: Root task and agent hop aggregation
Usage from child tasks, parent tasks, and agent hops SHALL roll up to the Root Task. A Root Task SHALL aggregate its own cost plus all descendant task and agent-hop cost, and the breakdown by hop SHALL be attributable.

#### Scenario: Root task total
- **WHEN** a multi-step task spans several agent hops
- **THEN** the Root Task total equals the sum of its model, tool, and agent-hop costs
- **AND** per-hop breakdown is queryable

### Requirement: Quantified optimization cost
Cache savings, retry cost, and fallback cost SHALL be derivable from Usage facts so the impact of routing and cache policies can be compared on benchmark traffic.

#### Scenario: Fallback cost quantified
- **WHEN** a request falls back across deployments
- **THEN** the additional fallback cost is attributable
- **AND** benchmark comparisons can show the policy effect
