## Context

LiteAIG already exposes `/healthz` and `/readyz`, flips readiness before drain,
and accepts explicit drain/stream/force-shutdown timeouts. The repository has no
Kubernetes manifests, so Standard/Enterprise cannot currently encode the V8.2
§30.11 disruption, placement, rollout, and autoscaling invariants. The chart is
a deployment adapter around the existing binary modes; it must not introduce a
second runtime configuration system.

## Goals / Non-Goals

**Goals:**
- Provide one minimal Helm chart supporting `all`, `gateway`, and `control`
  workloads with validated Standard/Enterprise HA defaults.
- Couple probes, termination grace, rollout, PDB, topology, resources, and HPA
  to the existing runtime contracts.
- Make environment-specific image, replicas, AZ topology key, resources,
  metrics, service, and secret references values-driven with explicit defaults.
- Add deterministic render/policy tests and Scenario G placement assertions.

**Non-Goals:**
- Provision clusters, load balancers, DNS/GSLB, PostgreSQL, Valkey, KMS, or
  object storage.
- Implement cross-Region RuntimeBundle replication or backup/restore.
- Put plaintext credentials in values, ConfigMaps, tests, or rendered evidence.

## Decisions

### 1. One chart, explicit workload declarations
The chart uses a values-driven workload list with stable modes (`gateway`,
`control`, or the default combined `all`). Each workload renders a Deployment,
Service, PDB, and optional HPA from the same helpers. This avoids duplicated
charts while preserving separate scaling and disruption policies. A fully
generic arbitrary-resource chart was rejected because it would weaken the
product invariants and validation surface.

### 2. HA defaults are safe but overridable
Standard defaults are three gateway replicas with PDB `minAvailable: 2`, and two
control replicas with `minAvailable: 1`. Required zone topology spread uses
`ScheduleAnyway` by default so partial-zone clusters remain deployable; strict
`DoNotSchedule` is opt-in. Preferred pod anti-affinity reduces correlated loss
without making small clusters unschedulable. Replica, AZ key, skew, resources,
rollout, and PDB values remain configurable.

### 3. Template validation handles relational invariants
`values.schema.json` validates types, enums, and bounds. Helm template helpers
use `fail` for relational rules JSON Schema cannot express: PDB availability
must be below replicas, `maxUnavailable` cannot remove every replica, and
termination grace must cover force shutdown. Invalid HA values fail rendering
rather than silently degrading availability.

### 4. Probes and drain reuse runtime endpoints
Startup/liveness use `/healthz`; readiness uses `/readyz`. The container receives
the existing drain flags and Kubernetes sends SIGTERM. `terminationGracePeriodSeconds`
must exceed the configured force-shutdown timeout; no shell `preStop` sleep is
used because it delays readiness withdrawal and duplicates lifecycle logic.

### 5. HPA is autoscaling/v2 and metrics are extensible
CPU utilization is the default metric. Additional resource, Pods, or External
metrics are supplied as validated values, enabling inflight requests and active
streams without hard-coding a metrics backend. Scale-down stabilization defaults
to a drain-aware window. Installing Prometheus adapters is out of scope.

### 6. Secrets remain references
The chart accepts existing Secret names or external Secret Provider annotations;
it never carries plaintext secret defaults. ConfigMaps contain only non-secret
runtime flags. Chart tests scan rendered manifests for prohibited credential
keys and inline Secret data.

## Risks / Trade-offs

- [HA defaults exceed a one-node development cluster] → Lite remains outside
  this chart baseline; values can lower replicas only when validation remains
  internally consistent.
- [Custom HPA metrics unavailable] → CPU remains a valid default and custom
  metrics are opt-in; missing metrics must not silently alter chart rendering.
- [Topology labels differ by platform] → topology key is configurable with the
  stable Kubernetes zone label as default.
- [PDB blocks voluntary maintenance] → validation ties PDB to replicas and the
  operator explicitly owns any stricter override.

## Migration Plan

1. Add chart metadata, values/schema, helpers, workload templates, and NOTES.
2. Add deterministic values-validation and rendered-manifest policy tests.
3. Render default, split-plane, and invalid configurations in CI.
4. Run Scenario G placement/rollout assertions and the repository gate set.

Rollback removes the Helm release or returns to the previous deployment
manifests; no application data or schema migration is introduced.

## Open Questions

- Which production metrics adapter will supply `inflight_requests` and
  `active_streams` is deployment-specific and remains an integration decision.
