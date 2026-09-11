# browser-management-data-links

## Why

Change 1 (`close-runnable-management-plane`) made the Lite management plane runnable, but several browser-facing operations still do not return real data or still report "not wired": the Playground writes mock facts instead of running through the production routing/execution pipeline, Live Tail has no publisher, and the Governance page's approvals / federated-agent suspend / agent graph surfaces return `errNotWired`. Projects can only be listed, not created. This change connects those data links so every page operation is backed by real services.

## What Changes

- **Playground runs the production pipeline**: `POST /api/admin/playground` and Setup's first call execute through a real gateway pipeline (routing planner + execution executor + mock provider invoker + accounting finalizer) instead of directly writing facts. Request Explorer receives route evidence, attempts, and usage for the same `request_id`.
- **Live Tail publishing**: request completion publishes summary events to the in-memory `LiveBus` so `GET /api/admin/live` emits real `request.summary` SSE events.
- **Governance backend wiring**: approvals (list + approve/reject), federated-agent suspend, and the agent/task graph are backed by real services (in-memory approval store, federation lifecycle, agent-graph builder over the accounting ledger), removing the remaining `errNotWired` responses on the Governance page.
- **Projects create**: `POST /api/admin/projects` creates a tenant project via the tenancy repository.
- **Config draft lifecycle** remains served by the config service (draft get/update/diff/publish/rollback/versions/rebase already wired in Change 1).

## Capabilities

### New Capabilities
- `browser-management-data-links`: Playground/Setup first-call run through the production pipeline with live-tailing; Governance approvals, federated-agent suspend, and agent graph are wired to real services; projects are creatable.

### Modified Capabilities
- `runnable-management-plane`: Governance page write/read surfaces no longer report "not wired".

## Impact

- **Backend**: `internal/app` gains a real pipeline (planner + executor + mock invoker + accounting + live bus), an in-memory approval store, federation lifecycle wiring, agent-graph builder wiring, and projects creation; `liteBackend`/`litePlayground` updated accordingly.
- **APIs/Console**: Playground returns production decision data; Governance approvals/federation/agent-graph render real data; Live Tail streams real events; projects can be created.
- **Dependencies**: no new third-party dependencies.
- **Tests**: pipeline-based playground test (route evidence + attempts + usage + live event), approvals + suspend + agent-graph backend tests, projects create test; existing gates stay green.

### Non-Goals
- Real upstream provider HTTP connectors (mock invoker remains the Lite baseline).
- PostgreSQL/Redis production composition, persistent (non-memory) approval store, cross-process Live Tail.
