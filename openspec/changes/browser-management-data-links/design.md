# browser-management-data-links: Design

## Context

Change 1 (`close-runnable-management-plane`) made the Lite management plane runnable: SQLite + migrations + Admin API + session + Console + Setup wizard. However several browser-facing data links were still fake: the Playground wrote mock facts directly instead of running through the production routing/execution pipeline, Live Tail had no publisher, the Governance page's approvals / federated-agent suspend / agent graph returned `errNotWired`, and Projects could not be created.

This change connects those data links with real services while keeping the Lite/mock-provider baseline.

## Goals / Non-Goals

**Goals:**
- Playground + Setup first call run through the real seven-stage pipeline (planner + executor + mock provider invoker + accounting), persisting route evidence, attempts, and usage.
- Request completion publishes to `adminapi.LiveBus`.
- Approvals, federated-agent suspend, and agent graph served by real services.
- `POST /api/admin/projects` creates a tenant project.

**Non-Goals:**
- Real upstream provider HTTP connectors (mock invoker remains the Lite baseline).
- Persistent (non-memory) approval store, cross-process Live Tail, PostgreSQL/Redis production composition.

## Decisions

### 1. Real pipeline via `pipeline.NewRunner`
`litePipeline` (internal/app) assembles the compile-time-fixed seven stages:
- Admission: pass-through (test principal set by `playground.Service`);
- InputGuardrail / OutputStreamGuardrail: `gateway/guardrail.Handler` over an empty builtin engine;
- PolicyCostPreflight: `gatewaycache.Handler` over a memory cache;
- Resolution: `routing.Planner` → sets `SelectedDeploymentID`;
- ExecutionResilience: `execution.Executor` with a mock `InteractionInvoker` via `staticResolver`;
- AccountingAndTelemetry: builds `accounting.Facts` from request + plan + execution result, finalizes through the sqlite accounting repository, and publishes a `LiveEvent`.

`gateway/playground.Service` builds the test-principal `RequestContext` and calls the pipeline, so both `POST /api/admin/playground` and Setup's first call share the same production path.

### 2. Governance via real services
- Approvals: `liteApprovals` wraps `approval.Service` over an in-memory `memoryApprovalStore`; wired via `SetApprovalLister`/`SetApprovalAction`.
- Federated-agent suspend: `liteFederation` wraps `federation.Lifecycle`; wired via `SetFederationSuspend`, and `liteBackend.Federation` overlays lifecycle status onto the snapshot view.
- Agent graph: `liteAgentGraph` rebuilds the per-root-task graph from the accounting ledger via `agentgraph.Builder`; wired via `SetAgentGraph`.

### 3. Projects creation
`CreateProject` added to the admin `Backend` interface + `POST /api/admin/projects` route; `ControlBackend.CreateProject` returns `errNotWired` (interface contract) while `liteBackend.CreateProject` delegates to the tenancy repository.

## Risks / Trade-offs

- [Memory approval store] → Lite-appropriate; a persistent store is a follow-up.
- [Mock invoker] → baseline decision; real connectors belong to the Standard profile.
- [Live Tail is per-process] → in-memory `LiveBus`; cross-process fan-out is a production concern.

## Migration Plan

1. `internal/app/pipeline.go` — real pipeline + mock invoker + static resolver.
2. `internal/app/governance.go` — approvals, agent graph, federation overlay.
3. Update `liteBackend` + `NewLite` composition; add `CreateProject` to the admin `Backend` interface and route.
4. Integration tests: playground pipeline + Request Explorer, approvals/agent-graph/federation, Live Tail SSE, projects create.
5. Run gates; record evidence.
