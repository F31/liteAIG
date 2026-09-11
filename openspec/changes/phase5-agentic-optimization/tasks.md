## 1. Agent Capability Routing

- [x] 1.1 Add `internal/agentic/routing`: `Router.Plan(ctx, snapshot, capability, metrics)` filtering `AgentsByCapability` and ordering by bounded health/cost/latency score.
- [x] 1.2 Add `AgentScore` with an evidence window and explainable components; scores never grant privilege.
- [x] 1.3 Add tests: capability filters endpoints, score orders eligible endpoints, score is evidence-windowed, production-plan equivalence (simulator uses the same Plan).

## 2. Agent Recommendations and Analytics

- [x] 2.1 Add `internal/agentic/recommend`: task-level agent routing/guardrail suggestions with an evidence window; accept creates a Config Draft via the existing `DraftCreator`.
- [x] 2.2 Add `internal/agentic/analytics`: per-agent cost/latency/loop statistics derived from `agentgraph.Graph`.
- [x] 2.3 Add tests: accept creates a draft and never mutates production, per-agent stats derived, no anomaly auto-applied.

## 3. Release Gates

- [x] 3.1 Add a Scenario F/B strengthening assertion (capability-driven agent selection with explainable scores; agent cost/latency optimization suggest-only).
- [x] 3.2 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures.
- [x] 3.3 Run `openspec validate --all --strict` and record Phase 5 DoD evidence.
