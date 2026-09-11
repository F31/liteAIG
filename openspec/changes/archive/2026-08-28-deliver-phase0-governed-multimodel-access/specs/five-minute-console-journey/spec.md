## ADDED Requirements

### Requirement: Guided first-call setup
The Console SHALL guide a new administrator through admin creation, provider configuration and connection test, model discovery or selection, Default Tenant and Project creation, Deployment creation, `default-chat` Logical Model creation, default Priority route, Virtual Key creation, first Playground call, SDK example, and Dashboard entry without requiring YAML or JSON editing.

#### Scenario: Five-minute first call
- **WHEN** a fresh Lite installation has valid provider credentials and the administrator follows the default wizard path
- **THEN** a real governed model call completes within five minutes
- **AND** the result displays request ID, tokens, available cost, latency, and selected deployment

#### Scenario: Provider connection test fails
- **WHEN** provider credentials or endpoint validation fails
- **THEN** the wizard shows a safe actionable error and retry step
- **AND** does not publish an unusable deployment or expose the secret

### Requirement: Playground uses the production pipeline
Playground SHALL use a short-lived test Principal and the same pipeline, route planner, guardrails, accounting, and connector contracts as production requests. It SHALL NOT require retrieval of a real Virtual Key secret.

#### Scenario: Playground request completes
- **WHEN** an administrator runs a Playground request against an active or explicitly selected Draft configuration
- **THEN** the request is marked `source=playground`
- **AND** provider usage and available cost are accounted

### Requirement: Request Explorer consistency
The Playground result SHALL link to a basic Request Explorer record using the same request ID. Route, deployment, usage, cost availability, latency, outcome, and configuration version SHALL agree with server-side request facts.

#### Scenario: Follow first request
- **WHEN** the administrator opens Request Explorer from the first Playground result
- **THEN** both views show the same request ID and decision facts
- **AND** the Explorer explains the selected deployment and any retry or fallback

### Requirement: Console draft safety
Provider, model, route, guardrail, limit, and other production configuration forms SHALL update server-side Drafts and expose validation and semantic diff before Publish. The active version SHALL be visually distinguishable from pending changes.

#### Scenario: Route edited but not published
- **WHEN** an administrator changes the default route and leaves the Draft unpublished
- **THEN** production requests continue using the active route
- **AND** the Draft bar shows pending changes

### Requirement: Browser secret safety
Provider and Virtual Key secrets SHALL exist only in transient component memory for the minimum necessary interaction. They MUST NOT enter localStorage, persisted application state, TanStack Query cache, logs, analytics, error reports, or service-worker caches.

#### Scenario: One-time key view closes
- **WHEN** the administrator closes the one-time Virtual Key secret view
- **THEN** no Console API can retrieve the complete secret again

### Requirement: Tenant-safe Console state
Tenant changes SHALL close scoped streams, cancel in-flight queries, clear tenant-scoped query cache and temporary Draft UI state, reset Project selection, fetch the authorized tenant context, and only then reconnect.

#### Scenario: Tenant switch
- **WHEN** a user who belongs to Tenant A and Tenant B switches from A to B
- **THEN** no Tenant A resource row or event is rendered in Tenant B context, including transiently

### Requirement: Accessible responsive Phase 0 Console
The Phase 0 Console SHALL support desktop and mobile-safe viewing for its required journey, use status indicators beyond color alone, and achieve Lighthouse Accessibility at least 90 with WCAG AA as the target.

#### Scenario: Accessibility release gate
- **WHEN** the Phase 0 Console runs its blocking accessibility suite
- **THEN** Lighthouse Accessibility is at least 90
- **AND** axe has no approved blocking violation

### Requirement: Golden Scenario A release gate
Phase 0 SHALL provide an automated Golden Scenario A using at least OpenAI-compatible and Anthropic protocol families and multiple model deployments. The release MUST fail unless a new application can call a Logical Model, change providers without business-code changes, and trace Tenant, Project, Key, Model, token, and cost-availability facts.

#### Scenario: Scenario A passes
- **WHEN** the Phase 0 release candidate runs Golden Scenario A
- **THEN** all compatibility, isolation, provider-switching, accounting, and traceability assertions pass without requiring a Provider Bridge
