# Deployment

Lite ships one self-contained static Go binary (`./cmd/liteaig`). The Admin
API, the data plane gateway, and the embedded Console are all served by it, so
**containerized and direct-process deployment are the same artifact, packaged
differently.** There is no separate runtime to install.

## The two supported models

| | Direct process | Containerized |
|---|---|---|
| Artifact | `go build -o liteaig ./cmd/liteaig` | `Dockerfile` → `ghcr.io/<repo>/liteaig:…` (distroless, non-root `65532`) |
| Start | `./liteaig --db file:/var/lib/liteaig/db.sqlite --admin-addr 127.0.0.1:8081 --gateway-addr :8082` | `docker run -v /var/lib/liteaig:/data image --db file:/data/liteaig.db …` |
| State | local SQLite file (+ `*.masterkey` sidecar) | mounted `/data` volume (SQLite), or Postgres + Redis for Standard tier |
| Scale | single process; split planes with `--mode=gateway\|control` | Helm chart `deploy/helm/liteaig` (Deployment/Service/HPA/PDB); split-plane replicas share Postgres + Redis coordination |

Notes that apply to both:

- Single-writer SQLite ⇒ exactly **one** `mode: all` replica may own a SQLite
  file. Replicated topologies must use the Standard tier (Postgres + Redis
  coordination) or split gateway/control planes that do not write the same file.
- Cross-instance coordination (`coordination_leases` + the durable A2A outbox
  atomic claim) is validated by `internal/app/a2a_two_process_smoke_test.go`:
  two real processes share one database and deliver each callback exactly once.
- Graceful shutdown: send SIGTERM; the process drains in-flight work before
  exiting (also what the `SIGTERM` container stop sends).

## Configuration

- CLI flags: `--db`, `--mode`, `--admin-addr`, `--ready-addr`, `--gateway-addr`,
  `--coordinator`, `--postgres-rls`, `--postgres-tenant-pools`, `--webhook-url`,
  SMTP flags, etc. (`--help`).
- AI traffic tuning: `--gateway-max-body-bytes` (default 1 MiB; raise for
  audio/vision/files), `--attempt-timeout`, `--total-timeout`,
  `--stream-total-timeout` (0 = no overall stream deadline),
  `--stream-idle-timeout`, `--first-token-timeout`, `--provider-timeout`.
- Cache tuning: `--exact-cache-capacity` (Lite only; ignored with
  `--coordinator`), `--semantic-cache-capacity` (default 1024),
  `--semantic-cache-ttl` (0 keeps vectors until LRU eviction).
- Layer 3 guardrail: `--external-guardrail-url`, `--external-guardrail-token`,
  `--external-guardrail-timeout`, `--external-guardrail-fail-mode`
  (`fail_open`, `fail_closed`, or `bypass`); the endpoint uses the same
  egress/SSRF policy as model connectors.
- Environment for secrets/telemetry: `LITEAIG_MASTER_KEY`, `LITEAIG_OTLP_ENDPOINT`,
  provider secrets (resolved via the secret provider / env refs).
- Health: `--ready-addr` serves `/healthz` (process) and `/readyz` (gated on a
  published tenant runtime); the container probes readiness with these.
- PostgreSQL RLS: enabled by default for PostgreSQL. Tenant-scoped data-plane
  and Admin requests use per-tenant pools as `liteaig_app` with
  `app.tenant_id` set on connect; control-plane/background work uses the
  trusted platform pool. The DB user needs permission to create/grant the
  `liteaig_app`/`liteaig_platform` roles. `--postgres-rls=false` is a
  controlled rollback to repository-scope-only isolation.
- Postgres connection budget: the platform pool is capped at 32 connections;
  each lazy tenant pool uses at most 2, and `--postgres-tenant-pools` (default
  16) bounds the number of tenant pools. On a default `max_connections=100`
  this keeps the worst case at 64; raise the pool count only after sizing
  Postgres accordingly.

## Delivery pipeline

- `ci.yml` `image` job builds the image and smoke-probes a container on every
  PR/push (non-root, mounted data dir, admin origin responding).
- `internal/app/gateway_load_smoke_test.go` is the PR-level gateway load gate:
  real auth/admission/routing/provider/accounting path, with RPS, p99 and error
  SLO floors.
- `ci.yml` also gates on `govulncheck`, production `npm audit`, and Kubernetes
  manifest validation (`scripts/check-k8s.sh`: Helm lint/template plus strict
  kubeconform in CI).
- `.github/dependabot.yml` opens weekly grouped PRs for Go modules, the Console
  npm tree, and GitHub Actions; the CI gates re-verify them on every PR.
- `release.yaml` builds on a `v*` tag, pushes to GHCR, attaches an SPDX SBOM,
  and files a GitHub Release.
- `deploy/helm/liteaig` for Kubernetes; `docs/A2A_TRUSTED_RELEASE.md` records the
  A2A release boundary and remaining external-runner items.
- `deploy/alerts/default-rules.json` ships the default alert ruleset contract;
  `internal/observability/alert/defaults_test.go` keeps it schema-valid. Import
  it through the Console Alerts page or `POST /api/admin/alerts/rules/import-defaults`.
- `docs/RELEASE.md` is the release operator checklist; `docs/UPGRADE_DRILL.md`
  is the migration and rollback rehearsal runbook; `docs/PRODUCTION_TRIAL.md`
  is the go/no-go gate, golden-signal threshold, and RPO/RTO checklist used
  during a single-tenant production trial.
- `docs/WHITEPAPER.md` is the product/architecture technical whitepaper;
  `docs/GAP_ANALYSIS.md` is the enterprise feature gap ledger and
  `docs/ROADMAP_V86_COMPLIANCE.md` records each batch's completion status.
- `docs/A2A_CONFORMANCE.md` and `docs/MODEL_SDK_CONFORMANCE.md` describe the
  networked official SDK compatibility gates.
- `scripts/kind-standard-e2e.sh` runs the Standard-tier Kubernetes smoke: kind
  cluster, Postgres, Redis coordinator, an in-cluster OpenAI-compatible mock
  provider, Helm multi-replica rollout, setup wizard initialization, readiness,
  and pod recovery after one replica is killed. In-cluster provider endpoints
  are reachable via `--egress-allow-cidrs` (self-hosted operators allow their
  Service CIDR; the default SSRF deny list otherwise stays in force).

## Local quickstart (direct process)

```bash
go build -o liteaig ./cmd/liteaig
./liteaig --db file:./liteaig.db --admin-addr 127.0.0.1:8081 --gateway-addr 127.0.0.1:8082
# open http://127.0.0.1:8081 and complete the setup wizard
```
