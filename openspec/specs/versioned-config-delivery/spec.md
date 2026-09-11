# versioned-config-delivery Specification

## Purpose
TBD - created by archiving change deliver-phase0-governed-multimodel-access. Update Purpose after archive.
## Requirements
### Requirement: Draft-first configuration changes
Every production behavior change SHALL be persisted in a server-side Draft or ChangeSet. Ordinary toggles and Console forms MUST NOT mutate the active runtime configuration directly.

#### Scenario: Toggle changes a rule
- **WHEN** an administrator changes an active configuration toggle
- **THEN** only the current Draft is updated
- **AND** the active tenant snapshot remains unchanged until publication

### Requirement: Validation before publication
Validation SHALL check schema, references, Tenant and Project scope, provider and credential ownership, Logical Model route viability, policy consistency, and secret references. Errors SHALL block publication; acknowledged warnings MAY proceed.

#### Scenario: Cross-tenant credential reference
- **WHEN** a Draft references another Tenant's private credential
- **THEN** validation reports a blocking scope error
- **AND** Publish remains unavailable

### Requirement: Semantic and secret-safe diff
The Control Plane SHALL produce a semantic diff between the active version and Draft. Secret values MUST NOT appear in diff payloads, logs, audit details, or Console query caches.

#### Scenario: Credential secret replacement
- **WHEN** a Draft replaces a provider credential secret reference
- **THEN** the diff reports that the secret reference changed
- **AND** neither old nor new plaintext secret is returned

### Requirement: Compile and atomically activate snapshots
Publish SHALL compile validated configuration into an immutable Tenant snapshot and atomically activate it only after durable version metadata exists. A compilation or activation failure SHALL leave the previous active snapshot serving traffic.

#### Scenario: Compilation failure
- **WHEN** compilation fails after validation
- **THEN** the Draft remains unpublished with diagnostics
- **AND** the previous snapshot version remains active

#### Scenario: Successful tenant publication
- **WHEN** a Tenant Draft is published successfully
- **THEN** only that Tenant's snapshot pointer is replaced
- **AND** new requests record the new version and security epoch as applicable

### Requirement: Versioned rollback
Rollback SHALL create, validate, and activate a new configuration version derived from a selected historical version. It MUST NOT delete or rewrite version history.

#### Scenario: Roll back a bad route
- **WHEN** an authorized administrator rolls back to a prior valid configuration
- **THEN** a new version becomes active with an audit link to the source version
- **AND** requests already in flight retain their originally captured snapshots

### Requirement: Auditable configuration lifecycle
Draft creation, validation, warning acknowledgement, publication, activation, failure, and rollback SHALL produce tenant-scoped audit events containing actor, timestamp, version, action, and result without secret values.

#### Scenario: Published change audit
- **WHEN** a Draft is published
- **THEN** Audit can identify who published which version and validation result
- **AND** Request Explorer can correlate later requests to that active version

