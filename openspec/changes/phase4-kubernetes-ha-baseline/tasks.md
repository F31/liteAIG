## 1. Chart Foundation and Validation

- [x] 1.1 Add `deploy/helm/liteaig` chart metadata, documented values, and `values.schema.json` for image, mode, replicas, topology, resources, rollout, drain, service, and existing Secret references.
- [x] 1.2 Add reusable naming/label and relational validation helpers; reject contradictory replica/PDB, rollout, and termination-grace settings with non-sensitive errors.
- [x] 1.3 Add chart validation tests for defaults, supported modes, invalid bounds, and absence of plaintext Secret data.

## 2. Workload Availability

- [x] 2.1 Render Deployment and Service resources with startup/readiness/liveness probes, existing drain flags, graceful termination, resource requests/limits, and bounded rolling-update settings.
- [x] 2.2 Render PodDisruptionBudget, zone topology spread, and preferred pod anti-affinity with selectors isolated to each workload.
- [x] 2.3 Add render tests for combined and split-plane workloads, PDB availability, probe paths, rollout bounds, zone-aware placement, and termination grace.

## 3. Drain-aware Autoscaling

- [x] 3.1 Render an `autoscaling/v2` HPA with CPU defaults, min/max bounds, scale-down stabilization, and values-driven custom metrics.
- [x] 3.2 Add tests proving default HPA validity, optional inflight/active-stream metrics, and disabled-autoscaling behavior.

## 4. Scenario G and Release Gates

- [x] 4.1 Add Scenario G policy assertions covering voluntary Pod disruption, rolling replacement, node/AZ placement, drain grace, and secret-minimizing manifests.
- [x] 4.2 Run Helm lint/render tests and the repository gate set; resolve blocking failures.
- [x] 4.3 Run `openspec validate --all --strict` and record Kubernetes HA baseline DoD evidence.
