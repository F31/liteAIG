# Phase 4 - Kubernetes HA Baseline: DoD Evidence

Status: Complete (11/11 tasks)

This change delivers the V8.2 §30.11 Standard/Enterprise Kubernetes HA baseline:
a minimal Helm chart with workload availability defaults, relational validation,
zone-aware placement, runtime probe/drain coupling, autoscaling, and
secret-minimizing rendered manifests.

## Capabilities Delivered

### Helm Chart Foundation (`kubernetes-ha-baseline`)
- `deploy/helm/liteaig`: Chart metadata, values, JSON schema, helpers, and
  templates for ServiceAccount, Deployment, Service, PodDisruptionBudget,
  HorizontalPodAutoscaler, optional PriorityClass, and NOTES.
- Values cover image, workload mode, replicas, topology, resources, rollout,
  drain, service, HPA, and existing Secret references.
- Relational Helm helper validation rejects contradictory PDB/replica,
  rollout/replica, HPA min/max, and termination-grace/force-shutdown settings.

### Availability and Placement
- Default gateway HA: 3 replicas, PDB `minAvailable: 2`, bounded rolling update,
  zone topology spread, preferred pod anti-affinity, resource requests/limits,
  `/healthz` startup/liveness probes, `/readyz` readiness probe, and drain flags.
- Split-plane control workload is available via values with separate replicas,
  PDB, service, HPA, and selectors.

### Drain-aware Autoscaling
- HPA uses `autoscaling/v2`, CPU metric by default, min/max bounds,
  scale-down stabilization, and values-driven custom metrics for integrations
  such as inflight requests and active streams.

## Gate Results

| Gate | Result |
| --- | --- |
| `helm lint deploy/helm/liteaig` (`/tmp/opencode/linux-amd64/helm` v3.16.2) | PASS |
| `helm template liteaig deploy/helm/liteaig` | PASS |
| `helm template ... --set workloads.control.enabled=true` | PASS |
| Invalid PDB/replica config | rejected with non-sensitive error |
| Invalid rollout/replica config | rejected with non-sensitive error |
| Invalid termination grace/drain config | rejected with non-sensitive error |
| `go test -race -count=1 -timeout 180s ./...` (Postgres 55432 + Redis 56379) | PASS after known chaos schema flake rerun |
| `go vet ./...` | PASS |
| `go run ./cmd/architecture-test` | PASS |
| `npm run check` (`web/console`) | PASS |
| `actionlint` | PASS |
| `gitleaks --config .gitleaks.toml detect --no-banner --redact` | no leaks found |
| `openspec validate --all --strict` | 24 passed, 0 failed |

## Tests

- `tests/kubernetes`: default HA values, JSON schema enums/bounds, helper fail
  messages, probe/drain wiring, rolling update, PDB selectors, topology spread,
  HPA defaults/custom metrics hook, secret-minimizing chart files, and Scenario G
  policy assertions.
- Helm render/lint: default and split-plane render successfully; invalid HA
  configurations fail before deployment.

## Scope Note

This chart is a deployment baseline around the existing single binary and
runtime readiness/drain contracts. It does not provision clusters, DNS/GSLB,
PostgreSQL/Valkey HA, KMS, object storage, cross-Region RuntimeBundle
replication, or DR backup/restore automation.
