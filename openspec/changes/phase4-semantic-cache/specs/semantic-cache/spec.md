# semantic-cache Specification

## ADDED Requirements

### Requirement: Tenant-scoped semantic cache
The system SHALL provide a Semantic Cache that embeds a normalized request, searches a tenant-scoped vector index, and returns the best candidate above a configurable similarity threshold. The index SHALL be namespaced by tenant, project, embedding model, dimension, and cache version so a cross-tenant candidate is never returned.

#### Scenario: Cross-tenant candidate never returned
- **WHEN** tenant A searches and tenant B has a similar stored vector
- **THEN** no tenant B candidate is returned to tenant A
- **AND** the search is confined to tenant A's namespace

#### Scenario: Threshold gates the candidate
- **WHEN** the best candidate is below the configured similarity threshold
- **THEN** the request is treated as a miss and the provider path is used

### Requirement: Cacheability gates
Semantic cache SHALL NOT cache tool calls, SHALL NOT serve or write requests blocked by a security policy, and SHALL bypass both lookup and write for an explicit `cache=false` or `Cache-Control: no-cache` directive.

#### Scenario: Tool call not semantic-cached
- **WHEN** a request contains a tool call
- **THEN** it is not written to or served from the semantic cache

#### Scenario: Security-blocked request not served
- **WHEN** a request is blocked by a security policy
- **THEN** it is never served from cache
- **AND** it is not written to the cache

### Requirement: Cache hit still runs output guardrail
A semantic cache hit SHALL short-circuit Resolution and Execution but SHALL still execute the current Output Guardrail and Accounting on the cached response, recorded as `source=semantic_cache`.

#### Scenario: Semantic hit is guarded
- **WHEN** a semantic cache candidate is served
- **THEN** the cached response is evaluated by the current Output Guardrail
- **AND** Accounting records the semantic cache hit

### Requirement: Semantic cache observability
The system SHALL expose low-cardinality semantic cache metrics (hits, misses) through the existing MetricSink and SHALL show semantic hits/misses on the Cache overview surface.

#### Scenario: Metrics reach the sink
- **WHEN** a semantic cache lookup completes
- **THEN** a `semantic_cache.hits` or `semantic_cache.misses` metric is recorded with no request-unique labels
