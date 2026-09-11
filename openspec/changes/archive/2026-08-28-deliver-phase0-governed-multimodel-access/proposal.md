## Why

LiteAIG currently has a frozen V7.2 architecture baseline but no executable product specification or implementation. The first delivery must prove the smallest complete enterprise outcome: an application can use one governed logical model across OpenAI-compatible and Anthropic providers within five minutes, while every request remains tenant-scoped, versioned, accounted, and explainable.

## What Changes

- Establish the Go modular-monolith runtime foundation, immutable tenant snapshots, fixed seven-stage pipeline, and executable architecture boundaries.
- Add Tenant, mandatory Default Project, one-time Virtual Key issuance, and snapshot-only Data Plane authentication.
- Add OpenAI Chat/Embeddings/Models, Anthropic Messages, and OpenAI-Compatible ingress and connector contracts behind Logical Models.
- Add Phase 0 Priority, Weighted, and RoundRobin routing with timeout, bounded retry, basic circuit exclusion, and fallback.
- Add Fast Guard, simple RPM, single-window budget, token usage, request records, routing decisions, and unconditional finalization.
- Add Draft, Validate, Diff, Publish, Activate, and Rollback configuration delivery without hot-path Control Plane reads.
- Add the Setup Wizard, Playground, basic Request Explorer, and minimum Console surfaces required to complete and verify the same first request.
- Add executable release gates for provider contracts, architecture rules, performance budgets, accessibility, and Golden Scenario A.

### Non-Goals

- Phase 1 distributed hardening, multi-window reservations, full circuit state machine, credential pools, RLS, OTel, alerts, and Live Tail.
- Exact or semantic cache, advanced/streaming/external guardrails, MCP, A2A, Agentic Governance, approvals, or Agent Graph.
- Cost-aware smart scoring, full FinOps/chargeback, Routing Simulator, native long-tail provider proliferation, or microservices.
- A generic API gateway, WAF, service mesh, plugin runtime, RAG pipeline, or Agent Runtime/Workflow/Planner.

## Capabilities

### New Capabilities
- `modular-runtime-foundation`: Fixed pipeline, runtime registry, stable contracts, module ownership, and architecture CI boundaries.
- `tenant-scoped-access`: Tenant/Project isolation, Virtual Key lifecycle, principal scope, and snapshot-only authentication.
- `multimodel-protocol-access`: OpenAI, Anthropic, and OpenAI-Compatible normalization and invocation through Logical Models.
- `basic-routing-resilience`: Deterministic Phase 0 route selection, timeout, retry, circuit exclusion, fallback, and decision evidence.
- `phase0-policy-accounting`: Fast Guard, simple RPM, single-window budget, token usage, request accounting, and telemetry events.
- `versioned-config-delivery`: Draft validation, semantic diff, compilation, atomic publication, activation, and rollback.
- `five-minute-console-journey`: Setup Wizard, Playground, Request Explorer, and Console workflow that proves the first governed call.

### Modified Capabilities

None. This repository has no previously published OpenSpec capabilities.

## Impact

- Introduces the initial Go module, backend package boundaries, persistence migrations, runtime compilation, HTTP APIs, embedded Console application, and test suites.
- Adds OpenAI, Anthropic, SQLite, PostgreSQL, and optional Valkey/Redis integration boundaries; Phase 0 acceptance must remain runnable in Lite mode without Redis.
- Establishes public compatibility behavior for selected OpenAI and Anthropic endpoints and internal stable Kernel/Connector/Event contracts.
- Golden Scenario mapping: Scenario A, Multi-model Unified Egress. Phase 0 is not complete until its end-to-end acceptance criteria pass.
