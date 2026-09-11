## MODIFIED Requirements

### Requirement: Cross-tenant isolation
All repositories, runtime indexes, resource references, logs, usage records, and API responses SHALL preserve Tenant scope. A Tenant A credential MUST NOT resolve or invoke Tenant B providers, deployments, models, keys, or request records. Phase 1 SHALL additionally enforce PostgreSQL RLS as a second defense layer for Standard/Enterprise repositories and Redis namespace isolation for coordination keys, as specified by `tenant-isolation-hardening`.

#### Scenario: Cross-tenant deployment reference
- **WHEN** Tenant A configuration references a Tenant B private credential or deployment
- **THEN** validation rejects the configuration before publication

#### Scenario: Cross-tenant request lookup
- **WHEN** a Tenant A principal requests a Tenant B request identifier
- **THEN** the API returns a non-disclosing not-found or forbidden response
- **AND** no Tenant B metadata is returned

#### Scenario: RLS blocks lateral access
- **WHEN** a Tenant-scoped query attempts to read another Tenant's rows under RLS
- **THEN** RLS returns no rows even if application-layer scope is bypassed
