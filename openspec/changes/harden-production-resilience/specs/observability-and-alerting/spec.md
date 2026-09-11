## ADDED Requirements

### Requirement: OpenTelemetry traces and spans
The Data Plane SHALL emit OTel spans for the request lifecycle and correlate them with Request IDs and Tenant/Project scopes. Business modules MUST NOT import the OTel exporter directly; they SHALL emit through the standard Event/Telemetry contract.

#### Scenario: Request span correlation
- **WHEN** a request completes
- **THEN** its spans correlate with the Request ID and the recorded snapshot version

### Requirement: Low-cardinality metrics
Prometheus/OTel metrics SHALL use only low-cardinality labels (provider, model, deployment, status, cache, guardrail). Per-request unique identifiers MUST NOT be used as metric labels.

#### Scenario: Metric label validation
- **WHEN** a metric is emitted
- **THEN** it carries no request-unique label values

### Requirement: External latency separation
Latency from external guardrails, semantic-cache embedding, and provider networks SHALL be recorded as separate durations and MUST NOT be folded into Core overhead.

#### Scenario: Provider latency isolated
- **WHEN** a provider request is slow
- **THEN** Core overhead and provider network duration are measured and reported separately

### Requirement: Alert Engine lifecycle
Alerts SHALL have a defined lifecycle (firing, acknowledged, silenced, resolved), a rule builder with typed thresholds, and notification channels configured as Webhook or Console. Alert state SHALL be scoped to Tenant where applicable.

#### Scenario: Budget threshold alert
- **WHEN** a configured budget crosses its threshold
- **THEN** an alert fires and can be acknowledged
- **AND** acknowledgement is recorded in Audit

### Requirement: Cost anomaly basic detection
The Alert Engine SHALL support basic cost-anomaly rules over aggregated Usage facts without requiring per-request content.

#### Scenario: Anomalous cost spike
- **WHEN** aggregated cost deviates beyond the configured anomaly rule
- **THEN** an alert fires with evidence window metadata
