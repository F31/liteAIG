# LiteAIG

**Enterprise AI Governance & Agentic Traffic Control Plane**

English | [简体中文](README.zh-CN.md)

LiteAIG is a modular-monolith, single-binary-first AI gateway. It puts model
calls, tool calls (MCP), and agent-to-agent collaboration (A2A) through one
**fixed seven-stage governance pipeline**, so identity, authorization, cost,
data residency, resilience, and audit semantics apply consistently to every
request. It ships as a self-contained Go binary with an embedded Console and
can grow from a single SQLite process (Lite) to PostgreSQL + Redis multi-replica
deployment (Standard) without changing the data-plane contract.

---

## Contents

- [Why LiteAIG](#why-liteaig)
- [Product capabilities](#product-capabilities)
- [Architecture](#architecture)
- [How LiteAIG compares](#how-liteaig-compares)
- [Quick start](#quick-start)
- [Security model](#security-model)
- [Project layout](#project-layout)
- [Testing and quality gates](#testing-and-quality-gates)
- [Documentation](#documentation)
- [Scope and limitations](#scope-and-limitations)
- [Contributing](#contributing)

## Why LiteAIG

Most AI gateways are excellent proxies. LiteAIG is designed as a **governance
control plane** first:

- **Governance is structural, not a plugin side effect.** All traffic is
  normalized into a canonical interaction model and passes the same pipeline.
  New capabilities attach as checkpoints; they cannot bypass admission,
  accounting, or audit.
- **Agentic traffic is a first-class citizen.** MCP tools/tasks and A2A
  agents are governed with the same keys, budgets, policies, approvals, and
  evidence as model calls.
- **Cost is part of correctness.** Budget reservations, RPM/TPM, token
  accounting, cache savings, and provider-cost attribution are built into the
  request path.
- **Operationally simple.** One static binary, an embedded Console, SQLite or
  PostgreSQL, and optional Redis coordination. No Python runtime, no separate
  UI service, no mandatory Kubernetes dependency.
- **Safe configuration delivery.** Tenant configuration is compiled into
  immutable runtime snapshots, signed into RuntimeBundles, activated
  atomically, and protected by a last-known-good fallback.

### At a glance

| | Lite | Standard |
|---|---|---|
| Store | SQLite, single writer | PostgreSQL |
| Coordination | In-process | PostgreSQL leases + Redis ledger/leases |
| Topology | One `mode=all` process | Split `gateway` / `control` replicas |
| Best for | Edge, lab, small teams, single tenant | Production HA, multi-tenant |
| Binary | One static Go binary with embedded Console | Same binary |
| Reference build | ~35 MB stripped binary; ~25 MB idle RSS | Horizontal scaling with external stores |

> Resource numbers are reference measurements from a local build, not a
> capacity guarantee. Benchmark your own topology and model mix.

---

## Product capabilities

### 1. Unified AI access

- Providers: OpenAI, Anthropic, Azure OpenAI, AWS Bedrock, Google Vertex AI,
  Gemini native, and OpenAI-compatible endpoints.
- Protocols: OpenAI Chat Completions, Responses, Embeddings, Rerank, Audio
  transcription/translation/speech, Files, Batch; Anthropic Messages; MCP
  JSON-RPC; A2A 0.3/1.0.
- Streaming SSE, tool/function calling, structured output, vision content
  parts, and multimodal request normalization.
- Logical models decouple application aliases from concrete deployments, so
  routing and provider changes do not require client changes.

### 2. Fixed seven-stage governance pipeline

Every model, MCP, and A2A request goes through:

1. **Admission** — authentication, virtual keys, rate/concurrency limits,
   request-size limits.
2. **Input Guardrail** — keyword/regex/secret/PII/prompt-injection rules,
   context guard, optional external content-safety service.
3. **Cache** — exact cache and semantic cache; a hit short-circuits execution
   but still records accounting and telemetry.
4. **Resolution** — model/tool/agent routing, data-residency constraints,
   budget reservation, capability checks.
5. **Execution Resilience** — retries, fallback, circuit breaking, credential
   pools, configurable stream/provider timeouts.
6. **OutputStream Guardrail** — inline/buffered/shadow streaming guard and
   optional LLM-as-Judge / groundedness checks.
7. **Accounting & Telemetry** — token metering, cost attribution, audit
   events, metrics, traces, and durable spooling.

### 3. Cost and FinOps

- Tenant → Project → Key budgets with distributed reservations in Standard
  tier.
- RPM/TPM limits, per-key quotas, concurrency leases, and in-flight tracking.
- Input/output/cache/reasoning/tool token accounting with provider usage
  reconciliation.
- Cache hit rate, avoided tokens, reference-price cost estimates, and
  unpriced-token reporting.
- Auxiliary embedding/judge calls are budgeted and recorded with a
  `parent_request_id` instead of disappearing outside FinOps.

### 4. Agentic governance

- **MCP**: tool catalog, per-tool policies, approvals, rate/budget limits,
  tool-call ledger, `tools/call` and `tasks/*` governance.
- **A2A**: agent endpoints, capability routing, trust relationships,
  delegation hops, federated identity, task idempotency, and exactly-once
  push outbox.
- Human approval and separation-of-duties workflows.
- Agent/task graph derived from the accounting ledger for audit and
  optimization.

### 5. Operations and security

- Embedded web Console: setup wizard, providers, models, keys, projects,
  policies, guardrails, cache, FinOps, alerts, audit, and agent views.
- Local Argon2id admins + OIDC SSO, CSRF protection, and role-based access.
- PostgreSQL RLS with tenant-scoped connection pools and `app.tenant_id`.
- Encrypted secrets, master-key sidecar for SQLite, and unified egress/SSRF
  policy for provider and guardrail calls.
- Signed RuntimeBundles, atomic activation, NACK keeps last-known-good.
- OTLP traces/metrics/logs, Prometheus `/metrics`, ClickHouse analytics sink,
  webhook/email alerts, and evidence export.
- Graceful drain, backup/restore scripts, DR drill CLI, and Kubernetes Helm
  chart with HPA/PDB.

### 6. Developer experience

- OpenAI-compatible API; existing SDKs work after changing the base URL and
  key.
- Official Go, JavaScript/TypeScript, and Python SDKs.
- OpenAPI contract, API-version headers, and compatibility gates in CI.
- One-command local startup and a built-in mock-provider test harness.

---

## Architecture

```mermaid
flowchart LR
  C[Clients: OpenAI / Anthropic / MCP / A2A] --> G[LiteAIG Gateway]
  A[Admin Console] --> CP[Control Plane]
  CP --> DB[(SQLite / PostgreSQL)]
  CP --> RB[Signed RuntimeBundle]
  RB --> G
  G --> P[Fixed 7-stage Pipeline]
  P --> RT[Routing & Resilience]
  RT --> PR[Model / Tool / Agent Providers]
  G <--> RD[(Redis coordination: Standard)]
  P --> O[OTLP / ClickHouse / Webhooks / Audit]
```

### Core components

| Component | Responsibility |
|---|---|
| `internal/kernel` | Canonical interaction model, pipeline contracts, runtime snapshot |
| `internal/access/protocol` | OpenAI/Anthropic/MCP/A2A wire adapters |
| `internal/gateway` | Admission, guardrails, cache, routing, execution, streaming |
| `internal/connectors` | Provider, tool, and agent connectors behind egress policy |
| `internal/controlplane` | Config compile/publish/rollback, Admin API, Console backend |
| `internal/finops` | Budgets, accounting, pricing, chargeback |
| `internal/federation` | A2A trust, delegation, push outbox |
| `internal/platform` | Storage, coordination, crypto, egress, observability |
| `web/console` | Embedded React Console |
| `cmd/liteaig` | Composition root and single-binary entry point |

### What is different

1. **Governance by construction.** The pipeline is fixed and CI-guarded.
   Extensions add checkpoints inside existing stages; there is no arbitrary
   stage reordering or bypass path.
2. **Compile once, sign, activate.** Configuration is validated and compiled
   into an immutable tenant snapshot. The control plane signs a RuntimeBundle;
   gateways activate it atomically and keep the last-known-good bundle if
   validation or signature checks fail.
3. **Two-tier delivery on one contract.** Lite uses SQLite and one process;
   Standard uses PostgreSQL + Redis and split planes. The data-plane behavior
   and governance semantics are the same.
4. **Agentic trust is not bolted on.** A2A/MCP share the same admission,
   budget, approval, accounting, and audit path as model calls.
5. **FinOps and audit are request-path features.** Budget reservation,
   token reconciliation, evidence, and cost attribution are not batch-only
   reporting.
6. **Small operational footprint.** A static Go binary, distroless non-root
   image, embedded Console, and no mandatory external runtime.

---

## How LiteAIG compares

> This table is a positioning aid, not a benchmark. These projects evolve
> quickly; verify current capabilities against their official documentation.

| Dimension | **LiteAIG** | **LiteLLM** | **Higress** | **Kong AI Gateway** |
|---|---|---|---|---|
| Primary positioning | AI governance & agentic traffic control plane | LLM proxy/SDK and provider abstraction | Cloud-native API gateway with AI plugins | Enterprise API gateway with AI plugins |
| Runtime | Go, single static binary | Python service/SDK | Envoy/Istio, Kubernetes-native | Nginx/OpenResty, Kong platform |
| Configuration | Compile-time validated snapshots + signed bundles | YAML + Python callbacks/hooks | Kubernetes CRDs + Wasm plugins | Kong entities + plugin config |
| Extensibility | Fixed pipeline + controlled checkpoints; compile-time registries and external guardrail hooks | Python ecosystem and callbacks | Wasm plugin ecosystem, CRDs | Large plugin ecosystem; Lua/Go/Python/JS |
| Provider breadth | OpenAI, Anthropic, Azure, Bedrock, Vertex, Gemini, compatible endpoints | Very broad multi-provider catalog | Broad AI provider support via AI plugins | Broad AI provider support via AI plugins |
| Governance depth | Fixed seven stages, budgets, TPM/RPM, RLS, audit, evidence | Virtual keys, budgets, rate limits, routing, caching, guardrails | L7 policies, token limits, WAF, AI guardrails/cache | AI rate limiting, prompt guard, PII, semantic cache |
| Agentic traffic | A2A 0.3/1.0, MCP tools/tasks, approvals, federation, trust | Not the primary focus | MCP/AI gateway features; A2A trust not primary | MCP Proxy and AI plugins; A2A trust not primary |
| Deployment | Binary, Docker, Helm; SQLite or PostgreSQL + Redis | Python service with DB/Redis | Kubernetes | Kong Gateway deployment |
| Sweet spot | Regulated AI governance, A2A/MCP control, FinOps, edge-to-HA | Fastest path to many providers in Python | Platform teams already on Envoy/Istio needing L7 + AI | Enterprises standardized on Kong |

**Trade-offs at a glance**

- **LiteAIG**: smaller provider catalog and plugin ecosystem than the
  incumbents; not a general L7/WAF gateway. Best when governance semantics
  and a small footprint dominate.
- **LiteLLM**: fastest path to a very broad provider catalog in Python; less
  opinionated about a fixed governance pipeline and agent trust.
- **Higress**: excellent Kubernetes/Envoy integration and plugin reach;
  requires a cloud-native data plane and Kubernetes operating model.
- **Kong AI Gateway**: mature enterprise gateway and plugin ecosystem; brings
  the Kong platform, database/control-plane operations, and licensing
  considerations.

**Choose LiteAIG when** governance, cost attribution, auditability, A2A/MCP
trust, and a small operational footprint matter more than having the largest
provider catalog or a general-purpose L7 plugin ecosystem.

**LiteAIG is not a replacement for a general API gateway.** It is designed to
work as an AI governance layer, optionally behind Kong, Higress, Envoy, or a
cloud load balancer.

---

## Quick start

### Prerequisites

- Go 1.25+
- Node.js 22+ (the Console assets are embedded into the Go binary and are not
  tracked, so a fresh source checkout must build them first; the Docker build
  does this automatically)
- Docker and Helm are optional

### Run from source (Lite)

```bash
git clone https://github.com/F31/liteAIG.git
cd liteAIG

# Build the embedded Console assets once (fresh clone)
(cd web/console && npm ci && npm run build)

go build -o liteaig ./cmd/liteaig

export LITEAIG_MASTER_KEY="$(openssl rand -base64 32)"
./liteaig \
  --db file:./liteaig.db \
  --admin-addr 127.0.0.1:8081 \
  --gateway-addr 127.0.0.1:8082
```

Open `http://127.0.0.1:8081` and complete the setup wizard. The wizard creates
the first tenant, provider, logical model, and virtual key.

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

The image runs as distroless non-root (`65532`). For production, terminate
TLS at a reverse proxy or ingress and restrict the Admin API to trusted
networks.

### Kubernetes / Standard tier

The Helm chart supports split `gateway` and `control` planes, HPA, PDB,
rolling updates, and drain settings. The Standard tier requires PostgreSQL and
Redis.

```bash
helm install liteaig ./deploy/helm/liteaig \
  -f ./deploy/helm/liteaig/examples/split-plane-values.yaml
```

Create the referenced Secret (`liteaig-standard`) with `database_dsn`,
`coordinator_url`, `liteaig_master_key`, `bundle_token`,
`bundle_signing_key`, and `bundle_public_key` before installing. See the
chart comments and [Deployment](docs/DEPLOYMENT.md) for production details.

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

## Security model

- Data-plane requests use virtual API keys; tenant snapshots are recompiled
  on key changes.
- Control plane supports local Argon2id admins and OIDC SSO with CSRF
  protection and RBAC.
- PostgreSQL RLS is enabled by default for tenant-scoped tables. Tenant
  requests use restricted `liteaig_app` pools with `app.tenant_id`; control
  plane/background tasks use the trusted platform pool.
- Secrets are encrypted at rest; provider and guardrail calls share a unified
  egress/SSRF policy.
- RuntimeBundles are signed and activated atomically; a bad bundle is rejected
  and the last-known-good snapshot keeps serving.
- Audit events, evidence export, structured security events, and OTLP logs are
  designed to avoid request bodies and secrets.

See [Security architecture](docs/WHITEPAPER.md) for details and operational
caveats.

---

## Project layout

```text
cmd/liteaig/                 single-binary entry point
internal/access/protocol/    OpenAI / Anthropic / MCP / A2A adapters
internal/gateway/            admission, guardrails, cache, routing, execution
internal/connectors/         model, tool, and agent connectors
internal/controlplane/       config lifecycle, Admin API, Console backend
internal/finops/             budgets, accounting, pricing, chargeback
internal/federation/         A2A trust, delegation, push outbox
internal/platform/           storage, coordination, crypto, egress, telemetry
internal/kernel/             interaction model, pipeline contracts, snapshots
web/console/                 embedded React Console
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

- Architecture boundary test (`cmd/architecture-test`)
- Gateway load smoke (`internal/app/gateway_load_smoke_test.go`)
- PostgreSQL RLS integration and end-to-end tests
- Official SDK conformance and OpenAPI compatibility checks
- Kubernetes kind Standard-tier smoke (Postgres + Redis + multi-replica)
- `govulncheck`, npm audit, Helm/Kubernetes manifest validation in CI

PostgreSQL-backed tests use `LITEAIG_TEST_POSTGRES_DSN`; Redis-backed tests
use the repository's coordination test environment.

---

## Documentation

- [Whitepaper: product and architecture](docs/WHITEPAPER.md)
- [Deployment guide](docs/DEPLOYMENT.md)
- [Gap analysis and roadmap](docs/GAP_ANALYSIS.md)
- [DR runbook](docs/DR_RUNBOOK.md)
- [API compatibility](docs/API_COMPATIBILITY.md)
- [SDK compatibility](docs/SDK_COMPATIBILITY.md)
- [A2A conformance](docs/A2A_CONFORMANCE.md)
- [Gateway benchmark](docs/GATEWAY_BENCHMARK.md)

---

## Scope and limitations

LiteAIG intentionally focuses on AI and agentic governance. It is **not** an
Agent runtime, RAG platform, Prompt IDE, model trainer, or general-purpose L7
gateway. Its extension model is controlled and compile-time-first; it does not
aim to replace Kong/Higress plugin ecosystems.

Current boundaries:

- Lite tier uses SQLite and is single-writer; use Standard for replicated
  production topologies.
- Semantic cache is bounded in-process by default; Standard exact cache and
  RPM/TPM use Redis, while a distributed vector index is still on the roadmap.
- Full multi-tenant SaaS, multi-AZ active traffic switching, SAML/SCIM, MCP
  task storage, and broader plugin bridges remain roadmap items. See
  [Gap analysis](docs/GAP_ANALYSIS.md) for the live list.

---

## Contributing

Issues and pull requests are welcome. Before submitting:

```bash
make test
make check
```

Keep the fixed pipeline invariant: new capabilities must attach as checkpoints
inside existing stages, and tenant-scoped data access must carry a
`TenantScope`/tenant context.
