# observability-and-alerting Specification (Delta)

## ADDED Requirements

### Requirement: Optional analytics forwarding
Domain Events SHALL be forwardable to an optional buffered Analytics Sink (ClickHouse-compatible) without coupling business modules, as specified by the analytics-sink capability. Analytics is never a request hot-path dependency.

#### Scenario: Events forwarded to analytics
- **WHEN** an Analytics Sink is configured
- **THEN** redacted Domain Events are forwarded to it in batches
- **AND** the request hot path is not coupled to the sink
