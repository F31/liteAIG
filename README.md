# LiteAIG

**Enterprise AI Governance & Agentic Traffic Control Plane**

English | [简体中文](README.zh-CN.md)

LiteAIG is a modular-monolith, single-binary-first AI gateway and governance
control plane. It puts **Model, Tool, Agent, and Human Approval** interactions
through one **fixed seven-stage pipeline**, so identity, authorization, cost,
data residency, resilience, safety, and audit semantics apply consistently to
every request.

This README reflects the **V8.5 implementation baseline** and the current state
of the code, not an aspirational feature list. The full specification is in
[LiteAIG_AI_Gateway_V8.5.md](docs/LiteAIG_AI_Gateway_V8.5.md); the live gap
ledger is in [GAP_ANALYSIS.md](docs/GAP_ANALYSIS.md).

---

## Contents

- [Product positioning](#product-positioning)
- [Architecture](#architecture)
- [Feature map](#feature-map)
- [How LiteAIG compares](#how-liteaig-compares)
- [Quick start](#quick-start)
- [Configuration and operations](#configuration-and-operations)
- [Implementation status](#implementation-status)
- [Project layout](#project-layout)
- [Testing and quality gates](#testing-and-quality-gates)
- [Documentation](#documentation)
- [Contributing](#contributing)

---

## Product positioning

### What LiteAIG is

LiteAIG is an **AI governance layer**, not another general-purpose API gateway.
It converges the operational differences between model providers, tools, and
agents into one controllable boundary, and provides:

- Multi-provider / multi-model access with Logical Models and Deployments.
- Explainable routing, credential pools, retries, fallback, and circuits.
- Exact and semantic cache with tenant isolation and output re-checking.
- Token metering, provider cost, chargeback, budgets, quotas, and rate limits.
- Guardrail lifecycle from policy authoring to test, preview, publish,
  enforcement, security events, and rollback.
- Request Explorer, Live Tail, Decision Timeline, Simulator, Config Diff,
  alerts, and audit.
- Unified governance for Model, MCP Tool, A2A Agent, and Human Approval.
- Agent identity, delegation, task/session/agent graph, loop control, and
  agentic FinOps.
- External / federated agent trust, bidirectional A2A governance, and
  procurement cost.
- Draft → Validate → Diff → Publish → Activate → Rollback configuration
  delivery with signed RuntimeBundles and last-known-good fallback.

### What LiteAIG is not

LiteAIG deliberately does **not** build:

- An Agent Runtime, Planner, Workflow, Memory, or Team Runtime.
- A RAG pipeline, vector database, or Prompt IDE.
- A model training, fine-tuning, or evaluation platform.
- A complete speech RTC platform.
- A general workflow engine or application marketplace.
- A general-purpose Ingress controller, Service Mesh, or WAF.
- An arbitrary Wasm/JS plugin runtime or a 100+ provider adapter race.

MCP and A2A are treated as **protocol adapters and governance objects**, not as
a second Agent platform. LiteAIG governs who may call what, under which budget,
boundary, and audit context; it does not decide how an agent plans its work.

### Deployment tiers

| Tier | Store / coordination | HA baseline | Recommended use | Design target |
|---|---|---|---|---|
| **Lite** | SQLite + in-memory cache | Single process, local LKG, graceful shutdown; no infrastructure HA | Development, small teams, edge | ≤200 RPS |
| **Standard** | PostgreSQL HA + Valkey/Redis HA | Gateway ≥3, Control ≥2, ≥2 AZ, LKG, accounting spool, PDB/topology spread | Mainstream production | ≤5,000 RPS per replica set |
| **Enterprise** | PostgreSQL Multi-AZ + Valkey/Redis Cluster | Active-active gateway/control, ≥3 AZ, region DR, optional multi-region active | Large multi-tenant / critical workloads | Horizontally scalable, 50k+ RPS target |

Capacity figures are architectural targets from V8.5, not SLA commitments.
Validate them with the benchmark and DR drills for your topology. HA is treated
as core correctness, not a separate feature toggle.

### Six leading product capabilities

| Capability | What it means in LiteAIG |
|---|---|
| **AI Access** | OpenAI/Anthropic-compatible access, mainstream native providers, OpenAI-compatible long tail, Logical Models, virtual keys, model ACL, setup wizard, provider test/model discovery. |
| **Explainable Smart Routing** | Hard constraints first (capability, context, ACL, region, residency, health), then cost/latency/load/quality/cache-affinity scoring; route explain trace and simulator share the production planner. |
| **Enterprise AI FinOps** | Usage Ledger as the source of truth; tenant/project/application/agent/key/org/user/model/provider attribution; budgets, quotas, rate limits, cache savings, retry/fallback cost, chargeback. |
| **Guardrail Lifecycle** | Policy → test case → impact preview → validate/diff → publish/security epoch → runtime enforcement → security event → retroactive analysis → optimization. |
| **AI Operations** | Playground, Request Explorer, Live Tail, Decision Timeline, Routing Simulator, Config Diff, Guardrail Impact Preview, health/circuit/alert, rollback. |
| **Agentic Governance & Audit** | Agent registry/version/endpoint/capability, delegation intersection, task budgets, loop detection, tool ACL, approval handoff, agent graph, federated trust, external procurement cost. |

---

## Architecture

LiteAIG is a **fixed-pipeline microkernel**. A small kernel owns the canonical
interaction model, the seven-stage pipeline, immutable tenant snapshots, and
controlled extension ports. Everything else is a governed component, adapter,
connector, store, or control-plane service. **Lite and Standard are deployment
profiles, not different architectures.**

### Architectural invariants

| Invariant | Implementation |
|---|---|
| **Modular monolith** | One `liteaig` binary; modules are ownership boundaries, not microservices. `mode=all\|gateway\|control` only changes the runtime role. |
| **Fixed pipeline** | Every interaction uses the same seven stages. New capabilities attach as checkpoints; no stage reordering or bypass. |
| **Kernel is business-neutral** | `internal/kernel` owns contracts, pipeline, runtime snapshot, and lifecycle. It does not import providers, policy, storage, control plane, or wire protocols. CI enforces this. |
| **WebKit microkernel** | Admin API and Gateway use `internal/platform/webkit` (Go `http.ServeMux`, middleware, APIError); no Echo/third-party web framework. |
| **Three-layer control plane** | `adminapi` (presentation) → `controlplane/backend` (orchestration) → `platform/storage` (persistence). Dependencies only point downward. |
| **Compile once, sign, activate** | Config is validated and compiled into an immutable tenant snapshot, signed as a RuntimeBundle, prepared/ACKed, then atomically activated. Invalid bundles keep the last known good. |
| **Control plane failure ≠ data plane failure** | A loaded data plane continues serving from LKG when the control plane, metadata DB, or PubSub is unavailable. |
| **Durable accounting** | Provider usage is written through a local accounting spool; ingestion is at-least-once with idempotent event IDs. The system does not claim fake exactly-once. |
| **Tenant is the isolation boundary** | Tenant snapshots, repository `TenantScope`, PostgreSQL RLS, Redis namespaces, cache namespaces, and scoped connection pools. |
| **Explicit degradation** | Cache, rate limit, budget, secret, DB, PubSub, and provider failures each have defined fail-open / fail-closed / bypass / fallback behavior. |

### Logical architecture

```mermaid
flowchart TB
  subgraph EDGE["Ingress"]
    C["Clients<br/>OpenAI SDK · Anthropic SDK · MCP · A2A"]
    FP["Optional TLS / WAF / L7 proxy"]
  end

  subgraph CP["Control Plane — optional split"]
    UI["Embedded Console / Admin API"]
    CFG["Config Compiler / Validator"]
    SIGN["Bundle Signer"]
  end

  subgraph DP["Data Plane — Lite: 1 process · Standard: N replicas"]
    AD["Protocol Adapters<br/>OpenAI | Anthropic | MCP | A2A"]
    PIPE["Fixed 7-Stage Pipeline<br/>Admission → Guardrail → Cache → Resolution<br/>→ Execution → Output Guardrail → Accounting"]
    CONN["Connectors<br/>Model · MCP Tool · A2A Agent"]
  end

  subgraph STATE["State & Operations"]
    DB[(SQLite / PostgreSQL<br/>RLS + tenant pools)]
    RD[(Redis — Standard<br/>budget · leases · cache)]
    SPOOL[(Accounting Spool)]
    OBS["OTLP · Prometheus · ClickHouse"]
  end

  C --> FP --> AD --> PIPE --> CONN
  CONN --> UP["Model / Tool / Agent Providers"]

  UI --> CFG --> SIGN
  SIGN -.->|RuntimeBundle (signed in Standard/Enterprise) + LKG| PIPE
  CFG --> DB

  PIPE <-->|Standard only| RD
  PIPE -.->|usage| SPOOL
  SPOOL --> DB
  PIPE -.->|audit / events| DB
  PIPE -.->|async telemetry| OBS
```

The diagram is intentionally compact. The control plane compiles configuration
and signs it in Standard/Enterprise (Lite can run from a local snapshot or
checksum). The data plane executes the fixed pipeline and uses external state
only for coordination and the durable asynchronous tail. Usage goes through the
accounting spool when enabled; audit and domain events persist through the DB
path. Solid edges are synchronous; dotted edges are control, deployment-specific,
or asynchronous.

### Fixed seven-stage pipeline

| # | Stage | Responsibilities |
|---|---|---|
| 1 | **Admission** | Authenticate virtual key, resolve tenant snapshot, enforce body limits, RPM/TPM, concurrency, and quotas. |
| 2 | **Input Guardrail** | Local fast rules, size/secret/PII/prompt-injection checks, context guard, optional external safety provider. |
| 3 | **Cache** | Exact and semantic cache; a hit short-circuits upstream execution but still runs output guardrail and accounting. |
| 4 | **Resolution** | Model/tool/agent resolution, hard constraints, data residency, budget reservation, capability and ACL checks. |
| 5 | **Execution & Resilience** | Provider/tool/agent invocation, stream handling, retries, fallback, circuit breaker, credential pool, configurable timeouts. |
| 6 | **Output Guardrail** | Output checks, streaming inline/buffered/shadow guard, optional LLM-as-Judge and groundedness checks. |
| 7 | **Accounting & Telemetry** | Token/cost metering, attribution, audit events, metrics, traces, durable spool, and outbox. |

### Four interaction types

| Interaction | Examples | Shared governance |
|---|---|---|
| **Model** | Chat, Responses, Embeddings, Rerank, Audio, Batch, Files | Keys, quotas, budgets, guardrails, routing, accounting, audit |
| **Tool** | MCP `tools/call`, `tasks/*` | Tool ACL, schema/DLP, approval, tool-call ledger |
| **Agent** | A2A 1.0 and 0.3-compatible traffic | Agent identity, delegation, task/hop limits, trust boundaries, federation |
| **Human Approval** | High-risk tool/agent actions | Short-lived grants, separation of duties, approval audit |

All four normalize into a canonical `InteractionContext`/`UnifiedRequest`
before entering the kernel. Protocol adapters do not make policy decisions.

### Service layering

| Layer | Package | Responsibility |
|---|---|---|
| Presentation | `internal/controlplane/adminapi` | HTTP routing, session, CSRF, RBAC middleware, error rendering, SSE framing |
| Business orchestration | `internal/controlplane/backend` | ControlBackend, 12 narrow capability interfaces, typed view contracts |
| Persistence | `internal/platform/storage` | `sqlrepo.Store`, SQLite/PostgreSQL adapters, spool |
| Composition root | `internal/app` | Opens stores, migrates, wires backend and HTTP servers |

Dependency direction is enforced by `architecture/forbidden-imports.yaml` and
`cmd/architecture-test`. The test also checks module ownership, table ownership,
migration owners, direct SQL references, and exported API compatibility.

### Runtime and config delivery

```text
Control Plane
  Draft -> Validate -> Diff -> Publish
    -> Compile Tenant Snapshot
    -> Sign RuntimeBundle
    -> Distribute
Data Plane
  -> Prepare (schema, signature, checksum, references)
  -> ACK / NACK
  -> Atomic Activate
  -> Keep previous + last-known-good on disk
```

A NACK never leaves a partially applied configuration. A gateway can start from
a valid signed LKG when the control plane is unreachable, while suspended
tenants and security fail-closed policies still win.

### Deployment profiles

| Aspect | Lite | Standard |
|---|---|---|
| Process topology | One `mode=all` process | `gateway` and `control` replicas |
| Store | SQLite + master-key sidecar | PostgreSQL + signed RuntimeBundle |
| Coordination | In-process | PostgreSQL leases + Redis ledger/leases |
| Cache | In-process exact + bounded semantic | Redis exact + in-process semantic |
| Failure behavior | Restart/recover from local state | CP outage keeps LKG; replica loss handled by HPA/PDB |
| Operations | Minimal single binary | HPA/PDB, drain, multi-replica, external stores |

Enterprise uses the same binary with multi-AZ PostgreSQL/Redis, three or more
AZs, and region DR. Full Enterprise HA/DR evidence remains on the roadmap and
must be validated by chaos/DR drills, not inferred from the deployment profile.

### Security and reliability boundaries

| Boundary | Controls |
|---|---|
| **Ingress** | Virtual API keys, body limits, optional TLS/WAF/L7 front proxy |
| **Admin** | Separate listener, local Argon2id or OIDC, CSRF, RBAC |
| **Tenant** | Immutable tenant snapshot, `TenantScope`, PostgreSQL RLS, scoped pools |
| **Egress** | Unified allowlist/SSRF policy, secret resolver, external guardrail isolation |
| **Supply chain** | Signed bundles, LKG, non-root distroless image, release SBOM |
| **Audit** | Redacted domain events, evidence export, durable spool/outbox, OTLP logs |

Dependency failure semantics are explicit: cache bypasses, soft budget may
fail open with alerts, hard budget/required security fails closed, provider
failures use circuit/fallback, and accounting is replayed idempotently.

---

## Feature map

### AI Access

- OpenAI Chat Completions, Responses, Embeddings, Rerank, Audio
  transcription/translation/speech, Files, Batch.
- Anthropic Messages.
- OpenAI-compatible long-tail endpoints; native OpenAI, Anthropic, Azure,
  Bedrock, Vertex, and Gemini connectors.
- Logical Model aliases decouple applications from deployments and providers.
- Virtual keys, tenant/project scope, model ACL, provider test, model discovery,
  and setup wizard.
- Official Go, JavaScript/TypeScript, and Python SDKs; existing OpenAI SDKs work
  by changing the base URL and key.

### Explainable Smart Routing

- Hard constraints: capability, context length, ACL, region, residency, health.
- Soft score: cost, latency, load, quality, cache affinity.
- Priority, weighted, round-robin, and runtime-aware plans.
- Retry, fallback, circuit breaker, credential pool, and configurable timeouts.
- Route explain trace; the simulator and production planner share the same
  implementation.

### Enterprise AI FinOps

- Usage Ledger with tenant/project/key/application/agent/org/user/model/provider
  attribution.
- Multi-window budgets, reservations, quotas, RPM/TPM, and concurrency leases.
- Provider cost, reference price, chargeback, cache savings, retry/fallback cost,
  and unpriced-token reporting.
- Auxiliary embedding/judge calls are budgeted and recorded with a
  `parent_request_id`.
- Attribution trust distinguishes verified, key-bound, and untrusted identity.

### Guardrail lifecycle

- Input, context, output, stream, tool/MCP, and agent guardrails.
- Keyword/regex/secret/PII/prompt-injection rules; optional external guardrail
  provider with `fail_open`, `fail_closed`, and `bypass` modes.
- Streaming guard uses inline/buffered/shadow layers; shadow findings are
  retroactive and never claim to retract already-sent content.
- Guardrail test cases, impact preview, security epochs, fast publish, security
  events, and retroactive analysis.
- Cache hits still pass the current output guardrail.

### AI Operations

- Embedded responsive Console/PWA: setup, dashboard, providers/models, keys, projects, routing,
  guardrails, cache, FinOps, alerts, audit, agents, tools, approvals, federation.
- Playground, Request Explorer, Live Tail, Decision Timeline, Routing Simulator,
  Config Diff, Guardrail Impact Preview, health/circuit views, and rollback.
- Prometheus `/metrics`, OTLP traces/metrics/logs, ClickHouse analytics sink,
  webhook/email alerts, and evidence export.
- Graceful drain, readiness separation, backup/restore scripts, DR drill CLI,
  and Helm HPA/PDB/topology spread.

### Agentic governance and audit

- MCP `2026-07-28` stateless Streamable HTTP; A2A `1.0.0` with 0.3-compatible
  wire handling.
- Agent registry, version, endpoint, capability, agent card, and trust anchors.
- Delegation is intersection-only; handoffs cannot escalate privileges.
- Task/root task/session/parent request linkage; budgets and limits for hops,
  model calls, tool calls, tokens, cost, and duration; loop detection.
- Tool ACL, schema/DLP, content provenance, tool-call ledger, and high-risk
  approval handoff.
- Inbound and outbound federated agent trust, capability/project grants, data
  boundary, external procurement budget, suspend/revoke, and security epochs.
- Agent/task graph derived from request, agent-call, and tool-call ledgers.

### Build / Integrate / Do Not Build

| Decision | Scope |
|---|---|
| **Build** | Tenant/project/identity scope, Logical Model/Deployment/Credential Pool, RuntimeSnapshot/config lifecycle, explainable routing, resilience, Usage Ledger/Budget/FinOps, guardrail lifecycle, AI operations, agentic governance/audit, federation trust. |
| **Integrate** | Long-tail providers via OpenAI-compatible or LiteLLM Bridge; Ingress/WAF via Higress/Envoy/Nginx; Prometheus/OTel/ClickHouse/Loki; OIDC/SAML/SCIM; KMS/Vault; webhook/email/Slack/Teams/PagerDuty; vector stores; A2A/MCP adapters; agent runtimes via protocol. |
| **Do not build** | 100+ native provider race, general API gateway/Ingress/Mesh/WAF, arbitrary Wasm/JS plugin runtime, RAG/vector DB, Agent Runtime/Planner/Workflow, Prompt IDE, training/fine-tuning platform, ERP/tax billing, IdP, MCP Tool Runtime, multi-agent planner/team runtime. |

---

## How LiteAIG compares

> This table is a positioning aid, not a benchmark. These projects evolve
> quickly; verify current capabilities against their official documentation.

| Dimension | **LiteAIG** | **LiteLLM** | **Higress** | **Kong AI Gateway** |
|---|---|---|---|---|
| Primary positioning | AI governance & agentic traffic control plane | LLM proxy/SDK + MCP/Agent gateway | Cloud-native API gateway with AI/MCP/Agent plugins | Enterprise AI Gateway with AI entities and policies |
| Runtime | Go, single static binary | Python service/SDK | Envoy/Istio, Kubernetes-native or standalone Docker | Nginx/OpenResty, Kong Gateway CP/DP |
| Configuration | Compile-time validated snapshots + signed bundles | YAML + Python callbacks/hooks | Kubernetes CRDs/Gateway API + Wasm | Kong entities + AI policies/plugins; decK/kongctl/Terraform |
| Extensibility | Fixed pipeline + controlled checkpoints; compile-time registries and external guardrail hooks | Python ecosystem and callbacks | Wasm plugin ecosystem, CRDs | Large plugin ecosystem; Lua/Go/Python/JS |
| Provider breadth | OpenAI, Anthropic, Azure, Bedrock, Vertex, Gemini, compatible endpoints | Very broad multi-provider catalog | Broad AI provider support via AI plugins | Broad AI provider support via AI plugins |
| Governance model | Fixed seven stages, budgets, TPM/RPM, RLS, audit, evidence | Virtual keys, budgets, rate limits, routing, caching, guardrails, async spend logging | L7/WAF, AI token limits, cache, content safety, quota | AI rate limiting, prompt/semantic guard, PII, semantic cache, cost management |
| Agentic / MCP | A2A 1.0/0.3 + MCP 2026-07-28, same governance path, approvals, federation/trust | MCP Gateway + Agent Gateway (A2A); router-centric | MCP Server + AI Agent; plugin-centric | AI MCP Server + A2A traffic + AI policies |
| Deployment | Binary, Docker, Helm; SQLite or PostgreSQL + Redis | Python service with DB/Redis | Kubernetes or Docker | Konnect or self-hosted Kong CP/DP |
| Sweet spot | Regulated AI governance, A2A/MCP control, FinOps, small footprint | Fastest path to many providers in Python | Platform teams on Envoy/Istio needing L7 + AI | Enterprises standardized on Kong |

**Trade-offs at a glance**

- **LiteAIG**: smaller provider catalog and plugin ecosystem than the
  incumbents; not a general L7/WAF gateway. Best when governance semantics and
  a small operational footprint dominate.
- **LiteLLM**: very broad provider catalog and a mature Python ecosystem; its
  governance model is proxy/router-centric rather than a fixed governance
  pipeline.
- **Higress**: excellent Kubernetes/Envoy integration and Wasm plugin reach;
  requires a cloud-native data plane and Kubernetes operating model.
- **Kong AI Gateway**: mature enterprise gateway, AI entities, and a large
  plugin ecosystem; brings the Kong platform, CP/DP operations, and licensing
  considerations.

**Choose LiteAIG when** governance, cost attribution, auditability, A2A/MCP
trust, and a small operational footprint matter more than having the largest
provider catalog or a general-purpose L7 plugin ecosystem.

**LiteAIG is not a replacement for a general API gateway.** Deploy it behind
Kong, Higress, Envoy, Nginx, or a cloud load balancer when those systems own
TLS/WAF/Ingress. LiteAIG can also use LiteLLM's OpenAI-compatible endpoint for long-tail
providers, but LiteAIG remains the final governance and metering entry point.

---

## Quick start

### Prerequisites

- Go 1.25+
- Node.js 22+ for a fresh source checkout: Console assets are embedded into the
  Go binary and are not tracked, so build them once. The Docker build does this
  automatically.
- Docker and Helm are optional.

### Run from source (Lite)

```bash
git clone https://github.com/F31/liteAIG.git
cd liteAIG

# Fresh checkout: build the embedded Console assets once
(cd web/console && npm ci && npm run build)

go build -o liteaig ./cmd/liteaig

export LITEAIG_MASTER_KEY="$(openssl rand -base64 32)"
./liteaig \
  --db file:./liteaig.db \
  --admin-addr 127.0.0.1:8081 \
  --gateway-addr 127.0.0.1:8082
```

Open `http://127.0.0.1:8081` and complete the setup wizard. It creates the first
tenant, provider, logical model, and virtual key.

```bash
export LITEAIG_KEY="<virtual-key-from-setup>"

curl -s http://127.0.0.1:8082/v1/chat/completions \
  -H "Authorization: Bearer $LITEAIG_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "default-chat",
    "messages": [{"role": "user", "content": "Hello from LiteAIG"}]
  }'
```

Streaming:

```bash
curl -N http://127.0.0.1:8082/v1/chat/completions \
  -H "Authorization: Bearer $LITEAIG_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "default-chat",
    "stream": true,
    "messages": [{"role": "user", "content": "Count to five"}]
  }'
```

### Docker

```bash
docker build -t liteaig .

docker run --rm \
  -p 8081:8081 -p 8082:8082 \
  -v liteaig-data:/data \
  -e LITEAIG_MASTER_KEY="$(openssl rand -base64 32)" \
  liteaig \
  --db file:/data/liteaig.db \
  --admin-addr 0.0.0.0:8081 \
  --gateway-addr 0.0.0.0:8082
```

The image runs as distroless non-root (`65532`). Terminate TLS at a reverse
proxy or ingress and restrict the Admin API to trusted networks.

### Kubernetes / Standard tier

The Helm chart supports split `gateway`/`control` planes, HPA, PDB, rolling
updates, topology spread, and drain settings. The Standard tier requires
PostgreSQL and Redis.

```bash
helm install liteaig ./deploy/helm/liteaig \
  -f ./deploy/helm/liteaig/examples/split-plane-values.yaml
```

Create the referenced Secret (`liteaig-standard`) with `database_dsn`,
`coordinator_url`, `liteaig_master_key`, `bundle_token`,
`bundle_signing_key`, and `bundle_public_key` before installing. See the chart
comments and [Deployment](docs/DEPLOYMENT.md) for production details.

### Configuration highlights

| Flag / env | Purpose |
|---|---|
| `--db` | SQLite DSN (Lite) or PostgreSQL DSN (Standard) |
| `--mode=all\|gateway\|control` | Single process or split planes |
| `--coordinator` | Redis URL for Standard-tier ledger/leases |
| `--postgres-rls` | Enable PostgreSQL RLS and tenant-scoped pools (default true) |
| `--postgres-tenant-pools` | Max lazy tenant pools (default 16, 2 connections each) |
| `--gateway-rpm`, `--gateway-tpm` | Request/token limits |
| `--gateway-max-body-bytes` | Raise for audio/vision/files |
| `--stream-total-timeout` | `0` = no overall stream deadline |
| `--stream-idle-timeout`, `--first-token-timeout`, `--provider-timeout` | AI traffic tuning |
| `--external-guardrail-url` | Layer 3 external content-safety service |
| `LITEAIG_MASTER_KEY` | Master key for secret encryption (required for PostgreSQL) |
| `--otlp-*` | OTLP traces/metrics/logs export |

---

## Configuration and operations

- **Health:** `/healthz` (process) and `/readyz` (accepting traffic, not
  draining, runtime loaded) are served on `--ready-addr`.
- **Metrics:** `/metrics` is served by the Admin origin; OTLP and ClickHouse
  sinks are optional and never required for data-plane readiness.
- **RLS:** PostgreSQL RLS is enabled by default. Tenant-scoped data-plane and
  Admin requests use `liteaig_app` pools with `app.tenant_id`; control-plane and
  background jobs use the trusted platform pool. `--postgres-rls=false` is a
  controlled rollback.
- **Connection budget:** the platform pool is capped at 32 connections; each
  lazy tenant pool uses at most 2, and `--postgres-tenant-pools` (default 16)
  bounds the number of tenant pools. Size this against `max_connections`.
- **Drain:** SIGTERM flips readiness, stops new traffic, allows normal requests
  and long streams to finish within the drain window, then finalizes accounting
  and exits.
- **Secrets:** provider secrets are encrypted at rest; the master key is
  required for PostgreSQL. KMS/Vault integration is an Integrate item and is
  not on the synchronous hot path.

---

## Implementation status

This table reflects the current codebase, not the full V8.5 target.

| Status | Items |
|---|---|
| **Implemented and tested** | Modular monolith, WebKit microkernel, three-layer control plane, fixed seven-stage pipeline, four interaction types, virtual keys/HMAC, RuntimeBundle/LKG/atomic activate, accounting spool, PostgreSQL RLS and tenant pools, Lite/Standard, embedded Console, exact/semantic cache, guardrail lifecycle, external guardrail fail modes, FinOps/budgets, A2A 1.0/0.3 + MCP 2026-07-28, approvals, federation trust, observability, architecture CI, gateway load smoke. |
| **Partially implemented** | Enterprise multi-AZ/region DR automation, distributed semantic-cache backend, SCIM (contract only), MCP task store/subscription, OrgUnit/User budget execution. |
| **Not yet implemented** | SAML, gRPC extension bridge, Slack/Teams/PagerDuty channels, FX/price feed, Admin API OpenAPI baseline, Terraform provider. |
| **Deferred / not built** | Full multi-tenant SaaS packaging, multi-region active/active, arbitrary plugin runtime, Agent Runtime/Planner/Workflow/Memory, RAG/vector database, general Ingress/WAF/Service Mesh, training/fine-tuning platform. |

The live feature-gap ledger and delivery phases are in
[GAP_ANALYSIS.md](docs/GAP_ANALYSIS.md) and
[ROADMAP_V86_COMPLIANCE.md](docs/ROADMAP_V86_COMPLIANCE.md).

---

## Project layout

```text
cmd/liteaig/                 single-binary entry point
cmd/architecture-test/       import/module/table/SQL boundary checks
architecture/                modules, forbidden imports, table owners
internal/kernel/             interaction, pipeline, runtime snapshot, contracts
internal/access/protocol/    OpenAI / Anthropic / MCP / A2A adapters
internal/gateway/            admission, guardrail, cache, routing, execution
internal/connectors/         model, tool, and agent connectors
internal/controlplane/       adminapi, backend, config, setup, RBAC, approval
internal/finops/             accounting, budget, pricing, aggregation
internal/federation/         trust relationships, anchors, grants
internal/platform/           webkit, storage, coordination, secrets, egress, bundle, LKG
web/console/                 React + TypeScript + Vite Console
deploy/helm/liteaig/         Kubernetes chart
migrations/                  SQLite/PostgreSQL schema
```

---

## Testing and quality gates

```bash
make test      # go test -race ./...
make check     # go vet + architecture boundaries + console checks
go build ./...
```

Notable gates:

- Architecture boundary test (`cmd/architecture-test`): module ownership,
  forbidden imports, table ownership, migration owners, direct SQL.
- Gateway load smoke (`internal/app/gateway_load_smoke_test.go`).
- PostgreSQL RLS integration and end-to-end tests.
- Official SDK conformance and OpenAPI compatibility checks.
- Kubernetes kind Standard-tier smoke (PostgreSQL + Redis + multi-replica).
- `govulncheck`, npm audit, Helm/Kubernetes manifest validation in CI.

PostgreSQL-backed tests use `LITEAIG_TEST_POSTGRES_DSN`; Redis-backed tests use
the repository's coordination test environment.

---

## Documentation

- [V8.5 implementation baseline](docs/LiteAIG_AI_Gateway_V8.5.md)
- [Whitepaper: product and architecture](docs/WHITEPAPER.md)
- [Deployment guide](docs/DEPLOYMENT.md)
- [Gap analysis and roadmap](docs/GAP_ANALYSIS.md)
- [DR runbook](docs/DR_RUNBOOK.md)
- [API compatibility](docs/API_COMPATIBILITY.md)
- [SDK compatibility](docs/SDK_COMPATIBILITY.md)
- [A2A conformance](docs/A2A_CONFORMANCE.md)
- [Gateway benchmark](docs/GATEWAY_BENCHMARK.md)

---

## Contributing

Issues and pull requests are welcome. Before submitting:

```bash
make test
make check
```

Keep the invariants: fixed pipeline, no arbitrary stage bypass, tenant-scoped
data access, signed runtime configuration, and architecture boundaries enforced
by CI.
