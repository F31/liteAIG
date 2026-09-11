## ADDED Requirements

### Requirement: PostgreSQL RLS defense layer
Standard/Enterprise Tenant repositories SHALL run under PostgreSQL Row Level Security as a second defense layer. The application connection MUST use a restricted RLS role, and platform operations MUST use a separately controlled role that does not bypass RLS through ordinary Tenant APIs.

#### Scenario: Lateral cross-tenant read blocked by RLS
- **WHEN** a Tenant-scoped query attempts to read another Tenant's rows under RLS
- **THEN** RLS returns no rows even if application-layer scope is bypassed

### Requirement: Redis namespace isolation
All Redis/Valkey keys SHALL be Tenant-namespaced. Cross-tenant coordination keys MUST NOT collide, and budget/lease/cache keys MUST include the Tenant namespace in the key name or hash tag.

#### Scenario: Distinct tenant namespaces
- **WHEN** two Tenants use the same logical key name
- **THEN** their Redis keys remain separate under distinct namespaces

### Requirement: Isolation regression suite
A security test suite SHALL verify horizontal RLS prevention, cross-tenant request/usage lookup denial, and Redis namespace isolation for every affected store.

#### Scenario: Isolation suite runs in CI
- **WHEN** the isolation suite executes
- **THEN** all lateral-movement and cross-tenant scenarios pass
