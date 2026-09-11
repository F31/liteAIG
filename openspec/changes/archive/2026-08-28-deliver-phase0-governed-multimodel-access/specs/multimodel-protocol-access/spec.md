## ADDED Requirements

### Requirement: Phase 0 protocol surface
The gateway SHALL support OpenAI `/v1/chat/completions`, `/v1/embeddings`, and `/v1/models`, Anthropic `/v1/messages`, and configurable OpenAI-Compatible upstream endpoints. Supported official SDKs SHALL require only credential and `base_url` changes for supported parameters.

#### Scenario: OpenAI-compatible chat
- **WHEN** an authenticated client sends a supported OpenAI chat completion request
- **THEN** the gateway returns the compatible success, stream, or error shape
- **AND** includes a LiteAIG request identifier suitable for Request Explorer lookup

#### Scenario: Anthropic message
- **WHEN** an authenticated client sends a supported Anthropic Messages request
- **THEN** the gateway preserves supported Anthropic request and response semantics
- **AND** governs the interaction through the same pipeline used by OpenAI traffic

### Requirement: Protocol normalization boundary
Ingress adapters SHALL only parse and validate wire requests, normalize them into `UnifiedRequest` and `InteractionContext`, invoke the Kernel pipeline, and normalize responses. Core and domain packages MUST NOT depend on OpenAI- or Anthropic-specific DTOs.

#### Scenario: Adapter dependency check
- **WHEN** architecture CI analyzes a protocol adapter
- **THEN** direct dependencies on policy, guardrail, routing, budget, FinOps, or connector implementations are rejected

### Requirement: Logical Model abstraction
Clients SHALL target tenant-scoped Logical Model names. A Logical Model SHALL resolve only to deployments visible in the authenticated Tenant snapshot, and changing its active deployment SHALL NOT require client code or client credential changes.

#### Scenario: Provider replacement without client change
- **WHEN** an administrator publishes a Logical Model route change from an OpenAI-compatible deployment to an Anthropic deployment
- **THEN** the same supported client request continues through the Logical Model without receiving either provider secret

### Requirement: Connector invocation contract
OpenAI, Anthropic, and OpenAI-Compatible connectors SHALL implement invoke, stream, health, capabilities, supported-parameter behavior, cancellation, and normalized upstream errors. Connectors MUST NOT authorize requests, select routes, read budgets, execute guardrails, or write the Usage Ledger.

#### Scenario: Connector cancellation
- **WHEN** the client cancels an in-flight non-streaming or streaming request
- **THEN** the connector promptly cancels the upstream request
- **AND** Accounting records the cancellation outcome

#### Scenario: Connector contract suite
- **WHEN** each connector runs its contract suite
- **THEN** normal, timeout, 429, 5xx, stream-drop, and client-cancellation fixtures pass with normalized outcomes

### Requirement: Supported parameter policy
Each connector SHALL declare supported parameters. Strict mode SHALL reject unsupported parameters with `UNSUPPORTED_PARAMETER`; permissive mode SHALL discard only safely ignorable parameters and record warnings. Unknown provider-specific passthrough SHALL require an explicit allowlist.

#### Scenario: Strict unsupported parameter
- **WHEN** a strict Project sends a parameter unsupported by every eligible deployment
- **THEN** Admission or Resolution returns HTTP 400 with `UNSUPPORTED_PARAMETER`
- **AND** no upstream connector is invoked

### Requirement: Tenant-filtered model discovery
`/v1/models` SHALL list only enabled Logical Models allowed for the authenticated Tenant, Project, key, and Principal; it MUST NOT expose physical provider credentials or inaccessible deployments.

#### Scenario: Scoped model listing
- **WHEN** a key limited to one Logical Model calls `/v1/models`
- **THEN** only that permitted Logical Model is returned
- **AND** no provider secret or foreign-tenant identifier appears
