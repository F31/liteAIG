## ADDED Requirements

### Requirement: Validated HA workload defaults
The Standard/Enterprise Helm chart SHALL render LiteAIG workloads with explicit
replica, resource, rolling-update, disruption, and graceful-termination
settings. It MUST reject configurations where a PodDisruptionBudget or rollout
setting can intentionally remove every available replica, or where termination
grace is shorter than the configured force-shutdown window.

#### Scenario: Default gateway remains available during voluntary disruption
- **WHEN** the default Standard gateway values are rendered
- **THEN** at least three replicas and a PodDisruptionBudget with at least two available replicas are declared
- **AND** rolling update permits replacement without making every replica unavailable

#### Scenario: Contradictory disruption policy is rejected
- **WHEN** PDB availability is greater than or equal to the configured replicas
- **THEN** Helm rendering fails with a non-sensitive validation error

### Requirement: Zone-aware placement
The chart SHALL configure topology spread across a values-driven zone topology
key and pod anti-affinity so one node or AZ loss does not place every replica in
the same failure domain. Placement strictness and skew SHALL be validated
configuration with safe defaults.

#### Scenario: Replicas spread across zones
- **WHEN** a multi-replica gateway or control workload is rendered
- **THEN** it includes a zone topology spread constraint and pod anti-affinity
- **AND** selectors match only that LiteAIG workload

### Requirement: Runtime health and drain coupling
The chart SHALL use the existing `/healthz` and `/readyz` runtime contracts for
startup, liveness, and readiness probes. Kubernetes termination and rollout
settings SHALL preserve the configured graceful drain and streaming shutdown
window.

#### Scenario: Draining pod leaves service before exit
- **WHEN** Kubernetes sends SIGTERM during a rollout
- **THEN** the LiteAIG process marks `/readyz` not ready before completing drain
- **AND** Kubernetes termination grace covers the force-shutdown timeout

### Requirement: Drain-aware autoscaling
The chart SHALL use `autoscaling/v2`, include a CPU metric by default, permit
validated custom inflight/stream metrics, and configure scale-down stabilization
so autoscaling does not immediately terminate multiple streaming pods.

#### Scenario: Default HPA is usable without a custom metrics adapter
- **WHEN** autoscaling is enabled with default values
- **THEN** the HPA contains a valid CPU resource metric and bounded min/max replicas
- **AND** custom metrics remain optional values-driven additions

### Requirement: Secret-minimizing rendered manifests
The chart MUST accept secret references without embedding plaintext credentials
in chart defaults, ConfigMaps, rendered test evidence, or NOTES output.

#### Scenario: Rendered manifests contain references only
- **WHEN** the chart is rendered using an existing Secret reference
- **THEN** workload configuration refers to the Secret by name
- **AND** no plaintext credential or inline Secret data is rendered

### Requirement: Scenario G Kubernetes evidence
The release gate SHALL render and validate default and split-plane values and
SHALL include policy assertions for voluntary disruption, rolling replacement,
node/AZ placement, probes, drain grace, and autoscaling.

#### Scenario: HA chart policy suite passes
- **WHEN** Scenario G Kubernetes checks run in CI
- **THEN** all default and split-plane manifests satisfy the HA invariants
- **AND** deliberately invalid PDB, rollout, and drain configurations are rejected
