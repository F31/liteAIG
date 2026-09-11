# Phase 5 — Agentic Optimization: DoD Evidence

Status: Complete (10/10 tasks)

This change delivers the P2 Agentic Optimization slice: capability-based Agent
Endpoint Routing with explainable scoring, suggest-only agent recommendations,
and read-only per-agent graph analytics. It does not evolve into an Agent
Planner/Workflow Runtime.

## Capabilities Delivered

### Agent Capability Routing (`agent-capability-routing`)
- `internal/agentic/routing`: `Router.Plan` filters `AgentsByCapability` and
  orders eligible endpoints by a bounded health/cost/latency score
  (`AgentScore` with an evidence window). The same Plan is used by production
  and the explainer (no second router).

### Agent Recommendations (`agent-optimization-recommendations`)
- `internal/agentic/recommend`: task-level agent routing/guardrail suggestions
  with an evidence window; `Accept` creates a Config Draft via a `DraftCreator`
  and never mutates production.
- `internal/agentic/analytics`: read-only per-agent cost/call statistics derived
  from the Agent/Task Graph; no anomaly is auto-applied.

### Modified Capabilities
- `agent-resource-model`: the compiled capability index now feeds production
  routing.
- `adaptive-governance-recommendation`: recommendations extend to agent routing.

## Gate Results

| Gate | Result |
| --- | --- |
| `go test -race -timeout 180s ./...` (Postgres 55432 + Redis 56379) | PASS |
| `go vet ./...` | PASS |
| `go run ./cmd/architecture-test` | PASS |
| `actionlint` | PASS |
| `gitleaks --config .gitleaks.toml detect` | no leaks found |
| `openspec validate --all --strict` | 22 passed, 0 failed |

## Tests

- Routing: capability filters endpoints, score orders eligible endpoints,
  production/explainer plan equivalence, no eligible agent error.
- Recommend: accept creates a draft and never mutates production, no-evidence
  rejected, cheaper-endpoint suggestion gated on cost delta.
- Analytics: per-agent cost/calls aggregated from the graph.
- Scenario F/B strengthening golden assertion: capability-driven agent
  selection with a suggest-only cheaper-endpoint recommendation.

## Scope Note

This is the P2 Phase 5 slice; per V8.2 it is not an initial GA gate. Capability
Routing, evidence-windowed scoring, suggest-only recommendations, and read-only
graph analytics are delivered. Agent Planner/Workflow/Memory, Multi-Agent Team
Runtime, and reputation auto-privilege remain explicitly out of scope.
