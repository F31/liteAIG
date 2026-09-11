## Context

The repository begins with the V7.2 design baseline and no application code. Phase 0 must establish architecture boundaries that later Guardrail, FinOps, MCP, A2A, and Agentic work can extend without creating a second pipeline or a distributed system prematurely.

The delivery unit is Golden Scenario A, not a collection of disconnected modules. The implementation must support a five-minute Lite/SQLite journey while preserving adapter boundaries for Standard/PostgreSQL. Redis is not required for Phase 0 correctness.

Frozen constraints from V7.2 include a Go modular monolith, one binary by default, a compile-time-fixed seven-stage pipeline, immutable per-tenant runtime snapshots, canonical Principal ownership, protocol/connector separation, one-table-one-owner, server-side configuration drafts, and React/TypeScript Console technology.

## Goals / Non-Goals

**Goals:**

- Deliver one complete path from first setup through a governed multi-provider request and Request Explorer evidence.
- Establish stable Kernel, connector, repository, event, and runtime contracts with executable dependency rules.
- Make Tenant isolation and unconditional accounting correctness part of the initial implementation rather than later hardening.
- Keep Lite mode self-contained while ensuring persistence and coordination contracts do not block Standard mode.
- Produce deterministic, contract-tested protocol, route, and failure behavior.

**Non-Goals:**

- Distributed reservation or lease algorithms, complete circuit coordination, RLS, HA, or 24-hour soak guarantees.
- MCP, A2A, Agent Registry, Delegation, Task Governance, cache, advanced guardrails, full pricing/chargeback, or smart route scoring.
- Publicly stabilizing internal Agent APIs or allowing custom pipeline stages.
- Splitting modules into services or adding internal network boundaries.

## Decisions

### 0. Use the canonical repository module path

The Go module path is `github.com/F31/liteAIG`, matching the canonical repository at `https://github.com/F31/liteAIG.git`. Go imports, generated clients, architecture manifests, and build metadata SHALL preserve this exact path and casing.

Alternative considered: use a temporary local module path until publication. Rejected because changing it later would create avoidable import churn and could leak an obsolete path into public contracts.

### 0.1 Freeze the Phase 0 delivery contracts

The protocol compatibility baseline is the versioned wire fixture profile `phase0-2026-08-27`: OpenAI Chat Completions, Embeddings, and Models plus Anthropic Messages, including non-stream, stream, normalized error, and cancellation fixtures. Wire behavior is the normative contract. Official SDK smoke tests pin exact package versions in their test lockfiles when task 9.2 adds them; SDK implementation types never become Kernel contracts.

Phase 0 single-window Budget is denominated in total tokens. Estimation, reservation, and reconciliation use normalized input plus output tokens. Provider cost is optional indicative data from an administrator-configured static deployment price with `source=manual_phase0`; if absent, APIs and Console return cost as unavailable rather than zero. Financial chargeback remains Phase 3 scope.

The blocking Standard Core Overhead target is P99 `<=6ms`, using the component budgets in V7.2 section 31.1. The reference benchmark runner is Linux/amd64, Go 1.24.x, four dedicated vCPU, 8 GiB RAM, local PostgreSQL 17, no external provider network, at least 30 seconds warm-up, and at least 100,000 measured requests. Benchmark output records all environment and workload metadata. SQLite tests and PostgreSQL repository conformance run on every pull request; Valkey/Redis is not a Phase 0 dependency.

Alternative considered: budget in money and report unconfigured prices as zero. Rejected because Phase 0 lacks versioned financial pricing and zero would fabricate a cost fact. Alternative considered: make a specific SDK release the protocol contract. Rejected because SDK internals change independently from the supported wire surface.

### 1. Build one vertical slice behind narrow contracts

Create only the Phase 0 package paths needed by this change. Do not pre-create empty Phase 1-5 packages. `gateway` owns orchestration; domain packages own rules and data; `platform` implements repositories and sinks; `kernel` contains only stable execution contracts and context types.

Alternative considered: scaffold the full V7.2 directory tree. Rejected because empty packages imply ownership without executable behavior and increase change surface without helping Scenario A.

### 2. Enforce architecture with manifests and import analysis

Add `architecture/modules.yaml`, `architecture/forbidden-imports.yaml`, and `architecture/table-owners.yaml`. A deterministic architecture test reads Go imports and migration ownership metadata. The same check runs locally and in CI.

Alternative considered: rely on code review. Rejected because V7.2 explicitly requires executable boundaries and ownership checks.

### 3. Keep the Kernel business-neutral

Kernel defines stage identifiers and ordering, `RequestContext`, normalized interaction/request/response types, runtime registry interfaces, connector/event contracts, and stable errors. Stage implementations are injected by the gateway composition root. A single top-level request runner captures the snapshot and defers idempotent finalization.

Alternative considered: put default routing, auth, or accounting behavior in Kernel. Rejected because it would couple the most stable layer to changing domain rules.

### 4. Use immutable compiled views with atomic tenant activation

Control Plane repositories store editable resources and configuration versions. Validation produces diagnostics; compilation creates a complete immutable `TenantRuntimeSnapshot`; activation atomically swaps only that tenant's pointer after version metadata is durable. Requests never hold a mutable Draft reference.

Alternative considered: query normalized configuration tables on each request. Rejected due to hot-path latency, Control Plane availability coupling, and inconsistent in-flight decisions.

### 5. Use repository contracts with SQLite and PostgreSQL adapters

Domain repository methods require an explicit `TenantScope` for tenant-owned data. Migrations declare their owning module. Lite acceptance uses SQLite; repository contract tests run against SQLite and PostgreSQL where semantics differ. Phase 0 RPM, budget, round-robin, and circuit state are process-local and therefore documented as Lite/single-replica behavior.

Alternative considered: require Redis from the first release. Rejected because distributed coordination belongs to Phase 1 and would break the self-contained Lite profile.

### 6. Separate protocol normalization from upstream invocation

OpenAI and Anthropic ingress adapters translate wire DTOs into canonical requests. Model connectors translate canonical invocation requests into upstream protocol calls. Governance, routing, retries, and accounting remain outside both. Contract fixtures define supported request, response, stream, error, and cancellation behavior.

Alternative considered: route raw provider DTOs through the pipeline. Rejected because it leaks protocol concerns into all domains and prevents common governance.

### 7. Make Phase 0 routing deterministic

Priority uses ascending configured priority followed by stable deployment ID. Weighted uses deterministic weighted rendezvous from session ID when present, otherwise request ID. RoundRobin uses a snapshot-local atomic sequence over stable deployment order. Route plans are immutable and include all candidate/exclusion evidence before execution starts.

Alternative considered: process-global pseudo-random weighted choice. Rejected because requests and tests could not reproduce decisions.

### 8. Use bounded local resilience without claiming distributed guarantees

Timeout and retry use request context deadlines and a total-call budget. The basic circuit is process-local with the V7.2 sample, error-rate, and cooldown defaults and one guarded post-cooldown probe. A route plan contains fallback order, but execution rechecks local circuit/health before each attempt. Once stream bytes are committed, execution cannot replay the request.

Alternative considered: implement the complete distributed circuit and lease state machine. Rejected as Phase 1 scope.

### 9. Treat accounting as an idempotent terminal state transition

Each request has one generated ID and one finalization key. Request completion, usage facts, and budget reconciliation commit transactionally where supported. The finalizer tolerates duplicate calls and emits redacted events after durable facts are committed. Event sink failure is observable but does not rewrite an already determined client outcome.

Alternative considered: asynchronous best-effort usage writes only. Rejected because Usage and request facts are Phase 0 correctness requirements.

### 10. Deliver the Console as an embedded single frontend

Use React, TypeScript, Vite, Ant Design 5/ProComponents, TanStack Query, React Hook Form/Zod, and React Router. Build assets are route-split and embedded into the Go binary. Drafts are server state; secrets remain transient component state; Tenant switching clears all scoped client state before loading the next context.

Alternative considered: separate frontend deployment or micro-frontend modules. Rejected because the frozen design requires one embedded Console and no current independent deployment need exists.

## Risks / Trade-offs

- [Process-local RPM, budget, RoundRobin, and circuit state do not coordinate across replicas] -> Mark Phase 0 as Lite/single-replica semantics and replace these implementations behind contracts in Phase 1.
- [Deterministic weighted routing can correlate traffic patterns] -> Use a keyed hash seed held by the runtime and never expose it in decision evidence.
- [A complete Phase 0 vertical slice is large] -> Implement in walking-skeleton order and require each slice to include negative isolation and finalization tests.
- [Official SDK compatibility can drift] -> Pin a compatibility matrix and fixture corpus in the connector contract suite.
- [Static Phase 0 prices could be mistaken for financial chargeback] -> Label them indicative, preserve price source/version fields, and display unavailable when not configured.
- [Snapshot compilation may consume memory for large tenants] -> Benchmark representative tenant sizes and keep indexes immutable and tenant-local; do not optimize prematurely without measurements.
- [SQLite and PostgreSQL behavior can diverge] -> Keep SQL behind repository contracts and run shared conformance tests against both adapters.
- [Secret leakage through UI diagnostics] -> Centralize redaction, prohibit secrets in query caches and error payloads, and include automated browser/log assertions.

## Migration Plan

1. Initialize the Go module, architecture manifests, migration framework, and embedded Console build without exposing production endpoints.
2. Add foundational ownership tables and create a local-admin bootstrap path for a fresh Lite database.
3. Add Draft validation, snapshot compilation, and activation; bootstrap data goes through the same publication path.
4. Enable protocol endpoints only after key authentication, tenant scope, pipeline finalization, and request records are operational.
5. Add connectors behind disabled-by-default provider configurations and run contract fixtures before enabling the Setup Wizard.
6. Enable the Console journey and Golden Scenario A in release builds after all blocking gates pass.

Rollback during development removes the unreleased database and binary. After the first release, schema changes must be forward-compatible; configuration rollback creates a new version and does not downgrade database migrations.

## Open Questions

None for the Phase 0 foundation baseline. New unknowns discovered during implementation must be recorded here before dependent behavior is added.
