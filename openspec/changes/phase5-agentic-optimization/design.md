# Phase 5 — Agentic Optimization: Design

## Context

Phase 4 delivered the compiled Agent capability index
(`TenantRuntimeSnapshot.AgentsByCapability` + `AgentEndpoints`), the Agent/Task
Graph (`internal/observability/agentgraph`), and suggest-only recommendations
(`internal/controlplane/recommend`). Phase 5 adds Capability-based Agent
Routing with explainable scoring and Task-level agent recommendations, reusing
those pieces. It must not become a Planner/Workflow Runtime.

## Goals / Non-Goals

**Goals:**
- Capability-based Agent Endpoint Routing using the compiled index, ordered by
  health/cost/latency score with the soft-score normalization.
- Agent scoring with an evidence window; explainable; never auto-grants privilege.
- Task-level adaptive recommendations (routing/guardrail) that are suggest-only;
  accept creates a Config Draft.
- Cross-Agent Graph analytics (per-agent cost/latency/loop) derived from the
  Agent/Task Graph.
- Capability Router shares the same deterministic plan as production.

**Non-Goals:**
- Agent Planner/Workflow/Memory, Multi-Agent Team Runtime, Reputation auto-
  privilege, Multi-AZ/DR, gRPC Extension Bridge.

## Decisions

### 1. Capability routing is a snapshot-only, deterministic plan
New `internal/agentic/routing`: `Router` with `Plan(ctx, snapshot, capability,
metrics) (*AgentPlan, error)`. It filters `AgentsByCapability`, then orders
eligible endpoints by a bounded normalized score (health/cost/latency) reusing
the soft-score normalization from `internal/routing/model`. The same `Plan` is
used by production and the simulator/explainer.
Alternative considered: a black-box agent ranker. Rejected — V8.2 §8.8 keeps
routing explainable.

### 2. Scoring carries an evidence window and never grants privilege
`AgentScore` carries health, cost, latency, and quality components over an
evidence window. A score is input to routing order only; it never grants
elevated permission.

### 3. Recommendations are suggest-only and land as Drafts
New `internal/agentic/recommend` produces task-level suggestions (e.g., route to
a cheaper endpoint, tighten a guardrail) with an evidence window; `Accept`
creates a Config Draft via the existing `recommend.DraftCreator`, never mutating
production.

### 4. Graph analytics are read-only
`internal/agentic/analytics` computes per-agent cost/latency/loop statistics
from `agentgraph.Graph` hops. No anomaly is auto-applied.

## Risks / Trade-offs

- [Router divergence] → production Plan is the same function as the simulator; equivalence test.
- [Score over-trust] → evidence window + explainable components; never grants privilege.
- [Recommendation over-application] → accept only creates a Draft.
- [Analytics misapplied] → read-only; no auto-application.

## Migration Plan

1. Add `internal/agentic/routing` (capability router + scoring) with tests.
2. Add `internal/agentic/recommend` (task-level suggestions) with tests.
3. Add `internal/agentic/analytics` (graph-derived stats) with tests.
4. Add production-equivalence + Scenario F/B strengthening assertions; run the full gate set; validate OpenSpec strict; record DoD evidence.

Rollback: routing/recommendations default off; existing agent resolution unchanged.
