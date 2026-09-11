# Phase 5 — Agentic Optimization

## Why

V8.2 Phase 5 (P2, not an initial GA gate) adds Capability-based Agent Routing, Agent Health/Cost/Latency scoring, Task-level Adaptive Recommendations, and Agent Graph analytics — always explainable and suggest-only. It builds directly on Phase 4: the compiled capability index (`AgentEndpoints` + `AgentsByCapability`), the Agent/Task Graph, and the suggest-only Recommendation pattern. Phase 5 MUST NOT evolve into an Agent Planner/Workflow Runtime.

## What Changes

- Add Capability-based Agent Endpoint Routing: resolve an agent task to an endpoint by capability using the compiled capability index, with health/cost/latency scoring over eligible endpoints (reusing the soft-score pattern).
- Add Agent scoring: per-endpoint health, cost, latency, and quality scores over an evidence window, all explainable and never auto-granting high privilege.
- Add Task-level Adaptive Governance Recommendations (routing/guardrail) that are suggest-only: accepting creates a Config Draft (reusing `internal/controlplane/recommend`).
- Add cross-Agent Graph analytics: per-agent cost/latency/loop statistics derived from the Agent/Task Graph (no anomaly auto-application).
- Verify the Capability Router shares the same deterministic plan as production.

## Capabilities

### New Capabilities
- `agent-capability-routing`: capability-based agent endpoint routing with health/cost/latency scoring.
- `agent-optimization-recommendations`: suggest-only task-level routing/guardrail recommendations from agent metrics.

### Modified Capabilities
- `agent-resource-model`: the capability index now feeds production-equivalent agent endpoint routing.
- `adaptive-governance-recommendation`: recommendations extend to agent-level routing.

## Impact

- **Backend**: new `internal/agentic/routing` (capability router + scoring) and `internal/agentic/recommend` (task-level suggestions over the agent graph); reuses `internal/observability/agentgraph` and the soft-score normalization.
- **Dependencies**: no new third-party runtime dependency.
- **Tests**: capability routing selects by capability + score, production plan equivalence, scoring has an evidence window, recommendation accept creates a draft and never mutates production, per-agent graph analytics (cost/latency/loop), no Planner/Workflow Runtime.

### Non-Goals
- Agent Planner/Workflow/Memory, Multi-Agent Team Runtime, Reputation auto-privilege, Multi-AZ/DR orchestration, gRPC Extension Bridge.

**Golden Scenario:** strengthens Scenario F (capability-driven agent selection with explainable scores) and Scenario B (agent cost/latency optimization, suggest-only).
