# agent-task-graph Specification

## ADDED Requirements

### Requirement: Graph reconstruction from facts
The system SHALL reconstruct a tenant-scoped Agent/Task graph from request and usage facts that carry `task_id`, `root_task_id`, `parent_task_id`, `session_id`, and `agent_id`. Nodes SHALL represent tasks, agents, tools, and models; directed edges SHALL represent calls and delegations grouped by Root Task. There SHALL NOT be a second `agentic_spans` fact source for graph reconstruction.

#### Scenario: Multi-hop chain reconstructed
- **WHEN** a root task spans agent → tool → model calls
- **THEN** the graph shows the chain in hop order
- **AND** each edge records its deployment, outcome, cost, and provenance

### Requirement: Graph queries
The graph SHALL support querying a Root Task's full chain, per-hop cost/breakdown, and the set of agents that crossed a trust boundary. Ordering SHALL be deterministic.

#### Scenario: Root task cost equals per-hop sum
- **WHEN** a root task is queried
- **THEN** its total cost equals the sum of its descendant hops
- **AND** per-hop breakdown is available

#### Scenario: Cross-boundary agents listed
- **WHEN** a root task crossed a trust boundary
- **THEN** the agents involved are identifiable from the graph

### Requirement: Span-attribute reconstruction parity
The graph SHALL be reconstructible from OTel span task attributes alone, producing the same chain as reconstruction from persisted facts.

#### Scenario: Span attributes match facts
- **WHEN** a task is reconstructed from span attributes
- **THEN** the chain matches the facts-derived chain for the same root task

### Requirement: Tenant isolation
Graph queries SHALL be tenant-scoped; facts from one tenant SHALL NOT appear in another tenant's graph.

#### Scenario: Cross-tenant query returns nothing
- **WHEN** a tenant queries another tenant's root task
- **THEN** no graph data is returned
