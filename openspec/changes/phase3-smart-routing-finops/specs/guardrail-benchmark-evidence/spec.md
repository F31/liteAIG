# guardrail-benchmark-evidence Specification

## ADDED Requirements

### Requirement: Reproducible guardrail benchmark
The system SHALL provide a Guardrail Benchmark Harness that runs a versioned corpus through the builtin engine and reports precision, recall, false-positive rate, and false-negative rate by category and language. The report SHALL record the corpus version, thresholds, test parameters, and model version, and SHALL compare against the previous version's regression. Reports MUST NOT promise zero false positives or false negatives.

#### Scenario: Benchmark report reproducible
- **WHEN** the benchmark harness runs on a fixed corpus and version
- **THEN** the metrics are identical across runs
- **AND** the report includes the corpus/version and parameters

#### Scenario: Regression delta
- **WHEN** a guardrail change shifts precision or recall
- **THEN** the report surfaces the regression difference versus the prior version

### Requirement: Read-only evidence export
The system SHALL export, for an authorized role, an Evidence Export for a Tenant and time range containing config/policy version, Security Epoch, RBAC/role assignments, audit events, guardrail events, external-guardrail/provider processing destinations, secret-rotation records, deployment/release versions, and (Phase 4) federated relationship records. The export SHALL default to excluding prompt/response bodies and secrets.

#### Scenario: Evidence export excludes sensitive content
- **WHEN** an operator exports evidence
- **THEN** the archive contains no prompt/response bodies or secrets
- **AND** the export is read-only and gated by an authorized role
