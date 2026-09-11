# Phase 4 — Agent / Task Graph: Design

## Context

`accounting.RequestRecord` and `UsageRecord` already carry
`session_id/task_id/root_task_id/parent_task_id/agent_id` (persisted by
migrations 014/015) and `agent_version/agent_endpoint_id` (018/019). OTel spans
carry `liteaig.task_id/root_task_id/parent_task_id`. This change reconstructs the
Agent/Task call graph from those facts and exposes it in Request Explorer,
completing Workstream B Phase 4 without introducing a second trace fact source.

## Goals / Non-Goals

**Goals:**
- Build a tenant-scoped Agent/Task graph from request/usage facts with task
  linkage: nodes (task, agent, tool, model) and directed edges (call,
  delegation) grouped by Root Task.
- Queries: full root-task chain, per-hop cost/breakdown, agents that crossed a
  trust boundary, and span-attribute reconstruction parity.
- Localized Agent Graph panel in Request Explorer.

**Non-Goals:**
- OIDC/SAML/SCIM, ClickHouse sink, Multi-AZ/DR, Groundedness/LLM-as-Judge, gRPC
  Extension Bridge, Phase 5 graph analytics/anomaly.

## Decisions

### 1. The graph is derived from persisted facts, not a new store
New `internal/observability/agentgraph` builds a `Graph` from a slice of
`accounting.RequestRecord` (and optional `UsageRecord`) carrying task linkage.
Nodes are keyed by `agent:<id>` / `tool:<id>` / `model:<name>` / `task:<id>`;
edges are `call` (caller task/agent → callee) with the request's deployment,
outcome, cost, and provenance. Grouping by Root Task aggregates hops. There is
no second `agentic_spans` fact source (V8.2 §36.2 Workstream B).
Alternative considered: a dedicated graph table. Rejected — deriving from the
ledger keeps one source of truth and matches the Frozen Observability decision.

### 2. Queries are tenant-scoped and deterministic
`Graph.RootChain(rootTaskID)`, `Graph.PerHopCost(rootTaskID)`,
`Graph.CrossBoundaryAgents()`, and `Graph.FromSpanAttributes(attrs)` (a builder
over span task attributes) all operate within one tenant scope. Ordering is
deterministic by hop sequence derived from parent-task linkage.

### 3. Console panel is read-only and localized
Request Explorer gains an Agent Graph panel that calls a tenant-scoped read
endpoint and renders the reconstructed chain, per-hop cost, and provenance —
no mutation. zh-CN/en-US parity, no hard-coded strings.

## Risks / Trade-offs

- [Malformed linkage] → orphan hops are grouped under their nearest root; validation skips nil task ids.
- [Large graphs] → bounded depth per root; pagination on the read endpoint.
- [Span-only reconstruction gap] → span attributes carry task/root/parent; reconstruction parity test asserts the same chain as facts.

## Migration Plan

1. Add `internal/observability/agentgraph` (types + builder + queries) with tests.
2. Add a tenant-scoped read API for a task's graph.
3. Add the localized Agent Graph panel to Request Explorer.
4. Add Scenario F/E strengthening assertions and run the full gate set; validate OpenSpec strict; record DoD evidence.

Rollback: the panel and endpoint are additive; facts already persisted.
