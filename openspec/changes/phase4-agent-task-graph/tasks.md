## 1. Graph Builder

- [x] 1.1 Add `internal/observability/agentgraph`: `Graph` types (nodes: task/agent/tool/model; edges: call/delegation with deployment, outcome, cost, provenance) built from `accounting.RequestRecord`/`UsageRecord` task linkage.
- [x] 1.2 Group edges by Root Task with deterministic hop ordering derived from parent-task linkage.
- [x] 1.3 Add `FromSpanAttributes` builder (task/root/parent span attributes) with no separate `agentic_spans` source.
- [x] 1.4 Add tests: multi-hop chain reconstruction, root-task total vs per-hop sum, cross-boundary agents, deterministic order.

## 2. Queries and Isolation

- [x] 2.1 Add `RootChain`, `PerHopCost`, `CrossBoundaryAgents` queries operating within one tenant scope.
- [x] 2.2 Enforce tenant isolation: a query for another tenant's root task returns nothing.
- [x] 2.3 Add span-attribute reconstruction parity test (same chain as facts).
- [x] 2.4 Add tenant-isolation tests and a malformed-linkage (orphan hop) tolerance test.

## 3. Console and Release Gates

- [x] 3.1 Add a tenant-scoped read endpoint for a task's Agent Graph and a localized Agent Graph panel in Request Explorer (zh-CN/en-US parity).
- [x] 3.2 Add a Scenario E/F strengthening assertion (agent→tool→result reconstruction; per-hop cost).
- [x] 3.3 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures.
- [x] 3.4 Run `openspec validate --all --strict` and record Phase 4 agent/task graph DoD evidence.
