# browser-management-data-links Specification

## ADDED Requirements

### Requirement: Playground runs the production pipeline
`POST /api/admin/playground` and the Setup wizard's first call SHALL execute through a real gateway pipeline (routing planner + execution executor + a provider `InteractionInvoker` + accounting finalizer) rather than directly writing mock facts. The persisted request record SHALL include route evidence, attempts, usage, and a `source=playground`.

#### Scenario: Playground produces a decision-backed request
- **WHEN** a playground request is made for an initialized tenant
- **THEN** the response includes `requestId`, `output`, `selectedDeployment`, token usage, and latency
- **AND** the persisted request record contains route evidence and attempts for the same `requestId`

#### Scenario: Live Tail receives the request summary
- **WHEN** a playground or Setup first-call request completes
- **THEN** `GET /api/admin/live` emits a `request.summary` SSE event for that `requestId` with outcome, deployment, and latency

### Requirement: Governance backend wiring
The Governance page's approvals, federated-agent suspend, and agent graph SHALL be served by real services instead of `errNotWired`:
- approvals: list inbox + approve/reject decisions through an approval service with separation-of-duties;
- federated-agent suspend: suspend a relationship through the federation lifecycle;
- agent graph: rebuild the per-root-task graph from the accounting ledger.

#### Scenario: Approvals render and decide
- **WHEN** a tenant has pending approvals
- **THEN** `GET /api/admin/approvals` lists them with requester, action, target, status, approver count, and dual-approval flag
- **AND** `POST /api/admin/approvals/{id}/action` applies approve/reject

#### Scenario: Agent graph renders from the ledger
- **WHEN** a root task has recorded requests
- **THEN** `GET /api/admin/agent-graph/{rootTaskId}` returns per-hop rows (order, agent, model, cost, outcome) and a total cost

### Requirement: Projects creation
`POST /api/admin/projects` SHALL create a tenant project through the tenancy repository and return the created project.

#### Scenario: Create a project
- **WHEN** a project create request is submitted for an authenticated tenant scope
- **THEN** a project is created and returned with id, name, status, residency enforcement, and allowed data regions
