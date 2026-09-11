# exact-cache Specification

## ADDED Requirements

### Requirement: Tenant-scoped exact cache with cache policy
The system SHALL provide an Exact Cache governed by a tenant-scoped Cache Policy compiled into the Tenant Runtime Snapshot. Cache keys SHALL include `tenant_id`, `project_id`, `logical_model`, the normalized request, semantic-affecting parameters, and a cache namespace version, and SHALL NOT be shared across Projects by default. Cache Policy settings (cacheability rules, TTL, namespace version) SHALL be validated typed configuration with explicit defaults.

#### Scenario: No cross-project sharing by default
- **WHEN** two Projects send identical normalized requests
- **THEN** each Project receives its own cache entry
- **AND** no response is served across the Project boundary

#### Scenario: Cache namespace invalidation
- **WHEN** an administrator publishes a new cache namespace version
- **THEN** cache entries keyed on the prior version are no longer served
- **AND** new requests use the new version for lookups and writes

### Requirement: Default cacheability rules
The system SHALL cache deterministic, low-temperature requests, embeddings, and classification/extraction/translation requests by default, and SHALL cache only when explicitly enabled by Policy or `cache=true`. The system SHALL NOT cache tool calls, high-temperature requests, Guardrail-blocked content, requests matching a high-sensitivity PII or secret policy, or requests carrying `Cache-Control: no-cache`.

#### Scenario: Tool call is not cached
- **WHEN** a request contains a tool call
- **THEN** the response is not stored in the Exact Cache

#### Scenario: No-cache directive honored
- **WHEN** a client sends `Cache-Control: no-cache`
- **THEN** the request bypasses cache read and write

### Requirement: Cache hit preserves governance
A cache hit SHALL short-circuit Resolution and Execution through the pipeline directive but SHALL still execute the current Output Guardrail and Accounting. Cached output SHALL be re-evaluated by the current Output Guardrail version, never served by-passing Stage 6 or Stage 7.

#### Scenario: Cache hit still runs output guardrail
- **WHEN** a cached response is served after a Guardrail policy Tighten
- **THEN** the cached response is evaluated by the current Output Guardrail
- **AND** the response is not returned if the current policy blocks it

#### Scenario: Cache hit records accounting
- **WHEN** a cached response is served
- **THEN** an Accounting record is written with the cache hit outcome
- **AND** cache savings are attributable

### Requirement: Cache savings and isolation observability
The system SHALL expose low-cardinality cache metrics (hit rate, savings, evictions) and SHALL keep cache keys and values tenant/project-isolated in both memory and Valkey stores. Cache values MUST NOT contain secrets.

#### Scenario: Tenant-isolated cache store
- **WHEN** a Tenant's cache is accessed
- **THEN** the underlying store is namespaced by tenant/project
- **AND** a cross-tenant cache lookup returns a miss rather than foreign data
