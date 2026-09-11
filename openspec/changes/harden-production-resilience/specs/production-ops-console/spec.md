## ADDED Requirements

### Requirement: Full Decision Timeline
Request Explorer SHALL render the complete decision timeline: Admission, Guardrail, Preflight, Resolution with evidence and exclusions, Execution attempts, Fallback/rotation events, Output Guardrail, and Accounting, correlated by request ID.

#### Scenario: Fallback explanation in timeline
- **WHEN** a request failed over to another deployment or credential
- **THEN** the timeline explains each attempt and the trigger

### Requirement: Request Live Tail
Live Tail SHALL stream request summaries over SSE with Last-Event-ID, reconnect with jitter, and heartbeat timeouts. Payloads SHALL contain summaries only and MUST NOT include Prompt/Response bodies.

#### Scenario: Live Tail privacy
- **WHEN** a Live Tail stream is observed
- **THEN** its payloads contain no prompt or response body
- **AND** the server enforces the current Tenant RBAC scope

### Requirement: Health and Circuits Console
The Console SHALL expose Provider/Deployment Health and Circuit state, allow authorized reset of circuit state as an operational action with confirmation, and reflect readiness and drift.

#### Scenario: Circuit reset requires confirmation
- **WHEN** an authorized operator resets a circuit
- **THEN** the action requires confirmation
- **AND** is recorded in Audit

### Requirement: Alert Inbox and rule builder
The Console SHALL list alerts with lifecycle actions (Ack, Silence, Resolve) and provide a basic typed rule builder. High-risk actions MUST require re-authentication.

#### Scenario: High-risk alert action
- **WHEN** an operator silences or resolves an alert classified high-risk
- **THEN** re-authentication is required before the action applies

### Requirement: RBAC scope navigation
Console navigation SHALL be authorized per Tenant/Project scope, and frontend RBAC SHALL be display-only while the backend enforces authorization.

#### Scenario: Scope-restricted navigation
- **WHEN** a user lacks access to a Tenant or Project
- **THEN** navigation and data for that scope are hidden
- **AND** direct API access is still rejected by the backend

### Requirement: Config Rebase and conflict
Concurrent Draft editing SHALL support rebase onto the latest active version and surface conflicts before publication.

#### Scenario: Concurrent draft conflict
- **WHEN** two drafts change the same resource and one is published first
- **THEN** the second draft surfaces a conflict and requires rebase before publish

### Requirement: PWA shell for operations
The PWA SHALL cache only static shell assets, support Dashboard, Alerts, and Requests summaries on mobile, and MUST NOT cache Admin API responses, Prompt, Response, Secret, or Usage detail offline.

#### Scenario: Offline shell without sensitive data
- **WHEN** the PWA is offline
- **THEN** only the static shell is available
- **AND** no Admin API, Prompt, Response, Secret, or Usage detail is cached
