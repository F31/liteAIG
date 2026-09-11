## Why

V8.2 §30.11 requires Standard/Enterprise deployments to survive routine Pod,
node, and single-AZ disruption, but the repository currently has no Helm or
Kubernetes delivery baseline. The runtime readiness/drain contracts now exist,
so the missing deployment layer can enforce them without inventing a second HA
mechanism.

## What Changes

- Add a minimal, validated Helm chart for gateway/control-plane deployment modes
  with startup/readiness/liveness probes and graceful termination.
- Add PodDisruptionBudget, topology spread, pod anti-affinity, zone-aware
  placement, rolling-update bounds, resources, and optional PriorityClass.
- Add HPA defaults for CPU plus extensible custom metrics such as inflight
  requests and active streams; scale-down behavior must respect drain time.
- Add chart validation that rejects contradictory replica/PDB/AZ/rolling-update
  settings instead of shipping an apparently HA but unschedulable deployment.
- Add rendered-manifest and policy tests that strengthen Scenario G with Pod,
  node, and single-AZ placement assertions.

## Capabilities

### New Capabilities
- `kubernetes-ha-baseline`: Standard/Enterprise Helm deployment defaults,
  topology/disruption/autoscaling policies, and validation contracts.

### Modified Capabilities

None.

## Impact

- Adds `deploy/helm/liteaig` templates, values, schema, and chart tests; no new
  runtime dependency and no secret material in chart defaults or rendered
  manifests.
- CI gains deterministic Helm render/lint and policy assertions. Console and
  localization are unaffected because this slice has no user-facing UI.
- Non-goals: provisioning Kubernetes clusters, managed PostgreSQL/Valkey HA,
  Global DNS/GSLB, cross-Region RuntimeBundle replication, and DR backup/restore
  automation. Those remain separate infrastructure or OpenSpec changes.
- Golden Scenario: Scenario G Enterprise HA, specifically rolling restart and
  single-AZ placement resilience.
