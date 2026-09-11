## 1. Real Playground Pipeline

- [x] 1.1 Add `internal/app` real pipeline assembly (`litePipeline`): routing planner + execution executor + mock provider `InteractionInvoker` + accounting finalizer; wire the seven fixed stages via `pipeline.NewRunner`.
- [x] 1.2 Route `POST /api/admin/playground` and Setup's first call through `gateway/playground.Service` backed by `litePipeline`; persist route evidence, attempts, and usage (source=playground) via the accounting repository.
- [x] 1.3 Publish request summaries to `adminapi.LiveBus` on completion so `GET /api/admin/live` emits real SSE events.

## 2. Governance Backend Wiring

- [x] 2.1 Approvals: add an in-memory `approval.Store`; wire `SetApprovalLister`/`SetApprovalAction` so `/api/admin/approvals` lists the inbox and `/api/admin/approvals/{id}/action` applies approve/reject with SoD.
- [x] 2.2 Federated-agent suspend: wire `SetFederationSuspend` to `federation.Lifecycle.Suspend`.
- [x] 2.3 Agent graph: wire `SetAgentGraph` to rebuild the per-root-task graph from the accounting ledger via `agentgraph.Builder`.

## 3. Projects Creation

- [x] 3.1 Add `CreateProject` to the admin `Backend` interface, a `POST /api/admin/projects` route, and a `liteBackend` implementation via the tenancy repository; update the test backend stub.
- [x] 3.2 Add an integration test covering project creation.

## 4. Release Gates

- [x] 4.1 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures.
- [x] 4.2 Run `openspec validate --all --strict` and record DoD evidence.
