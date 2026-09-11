# Phase 4 — Agent / Task Graph

## Why

V8.2 Workstream B (Agentic Observability) Phase 4 closes the loop with "Agent/Task Graph + OTel Span correlation". Phases 2-3 already persist `task_id/root_task_id/parent_task_id/session_id/agent_id` on request and usage facts and attach task attributes to OTel spans; this change reconstructs the Agent Graph from those facts so Request Explorer can answer "which user/agent called which agent/tool/model, why allowed/denied, and how much the root task cost". It is the observability foundation for Scenario F agent-hop cost and loop analysis.

## What Changes

- Add an Agent/Task Graph builder: given request/usage facts with task linkage, build nodes (agent, tool, model, task) and directed edges (call, delegation) grouped by Root Task, preserving per-hop cost and `crosses_trust_boundary`.
- Add graph queries: a Root Task's full call chain (agent → tool/model → result), per-hop cost/breakdown, and the set of agents that crossed a trust boundary.
- Expose the Agent Graph in Request Explorer (localized): for a request/task, show the reconstructed chain and each edge's evidence (deployment, outcome, cost, provenance).
- Verify OTel span correlation: task/root-task attributes on spans allow reconstructing the graph from spans alone (no separate `agentic_spans` fact source).

## Capabilities

### New Capabilities
- `agent-task-graph`: reconstructs and queries the Agent/Task call graph from Usage/Request facts.

### Modified Capabilities
- `agentic-identity-foundation`: session/task linkage now feeds graph reconstruction and per-hop cost attribution.

## Impact

- **Backend**: new `internal/observability/agentgraph` (builder + queries over `accounting.RequestRecord`/`UsageRecord`), a tenant-scoped read API, and a Scenario F strengthening assertion.
- **APIs/Console**: localized Agent Graph panel in Request Explorer (per-hop chain + cost + provenance).
- **Dependencies**: no new third-party runtime dependency.
- **Tests**: graph reconstruction from multi-hop facts, root-task total vs per-hop sum, cross-boundary hop flagged, tenant isolation, span-attribute reconstruction parity.

### Non-Goals
- OIDC/SAML/SCIM, ClickHouse sink, Multi-AZ/DR orchestration, Groundedness/LLM-as-Judge, gRPC Extension Bridge, Graph analytics/anomaly recommendation (Phase 5).

**Golden Scenario:** strengthens Scenario F (per-hop cost, agent graph reconstruction, loop/chain evidence) and Scenario E (agent→tool→result reconstruction).
