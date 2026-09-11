## ADDED Requirements

### Requirement: Tenant and mandatory project scope
The system SHALL treat Tenant as the highest business, data, security, and accounting isolation boundary. Every Tenant SHALL contain at least one Project, and single-team setup SHALL create a Default Project automatically.

#### Scenario: First tenant creation
- **WHEN** setup creates the first Tenant
- **THEN** the system creates a Default Project in the same transaction
- **AND** tenant-scoped resources cannot be created without a Project scope where one is required

### Requirement: High-entropy one-time Virtual Keys
Virtual Keys SHALL use `sk-lia-v1_<tenant_ref>_<public_id>_<secret>`, where `tenant_ref` and `public_id` are independent unguessable 128-bit values and `secret` has at least 256 bits of CSPRNG entropy. The secret SHALL be displayed only once and stored only as a versioned-pepper HMAC digest.

#### Scenario: Key creation
- **WHEN** an authorized administrator creates a Virtual Key
- **THEN** the complete key is returned exactly once
- **AND** subsequent APIs expose only metadata and a non-secret fingerprint

#### Scenario: Secret storage inspection
- **WHEN** persisted key data is inspected
- **THEN** no plaintext or reversibly encrypted Virtual Key secret is present
- **AND** the record identifies the pepper version used for verification

### Requirement: Snapshot-only key authentication
The Data Plane SHALL parse `tenant_ref` and `public_id`, locate the tenant and key through runtime indexes, verify the HMAC in constant time, and validate tenant status, key status, expiry, network policy, and scope without a per-request business database query.

#### Scenario: Valid key authentication
- **WHEN** a valid active Virtual Key is presented from an allowed source
- **THEN** Admission resolves its Tenant, Project, API Key, and bound Principal from the captured snapshot
- **AND** no Control Plane repository query occurs on the request path

#### Scenario: Invalid secret
- **WHEN** a token has a known `tenant_ref` and `public_id` but an invalid secret
- **THEN** authentication is denied using a non-enumerating error response
- **AND** no upstream connector is invoked

### Requirement: Key-bound scope cannot be overridden
Tenant, Project, Application, Agent, or Service Account identity bound to a Virtual Key SHALL NOT be replaced by untrusted request headers or payload fields. Client-reported user identifiers SHALL remain untrusted business labels.

#### Scenario: Project override attempt
- **WHEN** a request presents a Project identifier different from the authenticated key scope
- **THEN** the key-bound Project remains authoritative or the request is denied
- **AND** no resource from the requested foreign scope is disclosed

### Requirement: Cross-tenant isolation
All repositories, runtime indexes, resource references, logs, usage records, and API responses SHALL preserve Tenant scope. A Tenant A credential MUST NOT resolve or invoke Tenant B providers, deployments, models, keys, or request records.

#### Scenario: Cross-tenant deployment reference
- **WHEN** Tenant A configuration references a Tenant B private credential or deployment
- **THEN** validation rejects the configuration before publication

#### Scenario: Cross-tenant request lookup
- **WHEN** a Tenant A principal requests a Tenant B request identifier
- **THEN** the API returns a non-disclosing not-found or forbidden response
- **AND** no Tenant B metadata is returned
