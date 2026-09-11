# smart-routing-scoring Specification

## ADDED Requirements

### Requirement: Soft score over eligible deployments
Resolution SHALL first apply hard constraints (capability, context, ACL, region/residency, health, circuit, credential availability) and SHALL then order the eligible set by a configurable soft score combining latency, cost, load, and cache-affinity components. Component weights SHALL be compiled from the snapshot Route Policy with explicit defaults, and each component SHALL be normalized to a bounded range with a noise floor so small absolute differences do not dominate.

#### Scenario: Cost-aware selection
- **WHEN** two eligible deployments serve the same model and one has a lower cost weight-adjusted score
- **THEN** the lower-scoring deployment is selected first
- **AND** the decision evidence records the per-component scores

#### Scenario: Hard constraints still exclude first
- **WHEN** a deployment would minimize cost but violates a hard constraint (e.g., data residency)
- **THEN** it is excluded before scoring
- **AND** the exclusion is recorded in evidence

### Requirement: Explainable route score
Every route decision SHALL record the eligible candidates, their hard exclusions, and their per-component soft scores so Request Explorer can explain "why this deployment".

#### Scenario: Score evidence in decision
- **WHEN** a request resolves a route
- **THEN** the Route Plan evidence includes the selected deployment's score breakdown
- **AND** the weights and normalization used are identifiable from the snapshot version

### Requirement: Routing Simulator shares the production planner
A Routing Simulator SHALL replay requests or benchmark traffic through the same `PlanRoute()` implementation used by production, accepting overrides (weights, health, circuit) without mutating live state, and SHALL return decisions identical to production for the same input.

#### Scenario: Simulator reproduces production decision
- **WHEN** an operator runs the simulator with the same request and snapshot
- **THEN** the selected deployment and evidence match production exactly
- **AND** the simulator records that it ran in replay mode
