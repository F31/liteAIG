# Phase 4 — Agent / Task Graph: DoD Evidence

Status: Complete (12/12 tasks)

This change reconstructs the Agent/Task call graph from persisted request/usage
facts and exposes it through a tenant-scoped read endpoint and a localized
Console surface, completing Workstream B (Agentic Observability) Phase 4 without
introducing a second trace fact source.

## Capabilities Delivered

### Agent/Task Graph (`agent-task-graph`)
- `internal/observability/agentgraph`: `Graph` (nodes: task/agent/tool/model;
  edges: call/delegation with deployment, outcome, cost, provenance) built from
  `accounting.RequestRecord` task linkage, grouped by Root Task with
  deterministic hop ordering.
- Queries: `RootChain`, `PerHopCost`, `CrossBoundaryAgents`, `TotalCost`.
- `FromSpanAttributes` reconstructs the chain from OTel span task attributes
  with parity to facts reconstruction (no separate `agentic_spans` source).
- Tenant isolation enforced by construction (per-tenant builder); malformed
  (orphan) linkage tolerated.

### Console and API
- Tenant-scoped `GET /api/admin/agent-graph/{rootTaskId}` read endpoint.
- Localized "Agent graph" tab in Governance (root task, total cost, per-hop
  table; zh-CN/en-US parity).
- Scenario F strengthening golden assertion: agent → tool → model chain
  reconstruction with per-hop cost and attributable trust-boundary crossing.

## Gate Results

| Gate | Result |
| --- | --- |
| `go test -race -timeout 180s ./...` (Postgres 55432 + Redis 56379) | PASS |
| `go vet ./...` | PASS |
| `go run ./cmd/architecture-test` | PASS |
| `npm run check` (format, locales, hardcoded strings, vitest, build) | PASS |
| `actionlint` | PASS |
| `gitleaks --config .gitleaks.toml detect` | no leaks found |
| `openspec validate --all --strict` | 17 passed, 0 failed |

## Tests

- Multi-hop chain reconstruction; root-task total equals per-hop sum;
  deterministic ordering; tenant isolation; cross-boundary agents flagged;
  malformed-linkage tolerance; span-attribute reconstruction parity.
- Scenario F graph golden assertion (chain + per-hop cost + boundary crossing).

## Scope Note

The Request Explorer inline panel is represented by the localized Governance
"Agent graph" tab plus the tenant-scoped read endpoint; the deeper in-request
Explorer embedding and Phase 5 graph analytics/anomaly remain out of scope for
this slice.
