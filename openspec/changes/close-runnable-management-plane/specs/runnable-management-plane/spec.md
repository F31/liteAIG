# runnable-management-plane Specification

## ADDED Requirements

### Requirement: Lite composition root
The `internal/app` package SHALL provide a composition root for the Lite profile that opens SQLite, applies migrations, seeds an active key pepper, constructs the sqlite repositories and domain services, and returns an `http.Handler` exposing the Admin API, session endpoints, and the embedded Console. The composition root SHALL support graceful shutdown via the existing lifecycle drain.

#### Scenario: Initialize Lite storage
- **WHEN** a SQLite DSN is provided
- **THEN** the database is opened, migrations are applied, and an active key pepper exists
- **AND** no other storage backend is required

#### Scenario: Compose runnable handler
- **WHEN** the composition root is built
- **THEN** the returned handler serves `/api/admin/*`, session endpoints, and the Console SPA
- **AND** `/api/admin/me` responds after a valid session

### Requirement: Setup wizard closure
The Admin API `POST /api/admin/setup` SHALL run the Lite wizard end to end: bootstrap admin/tenant/default project, register a provider with a credential secret reference, verify connectivity, discover models, configure the default deployment and logical model, create a virtual key, and run a first call. The response SHALL include `virtualKey`, `sdkExample`, `requestId`, `tenantId`, and `projectId`.

#### Scenario: First installation creates a runnable tenant
- **WHEN** `POST /api/admin/setup` is called with valid wizard input
- **THEN** a tenant, default project, local admin, provider, credential, deployment, route policy, logical model, and virtual key are created
- **AND** a first request record is produced
- **AND** a subsequent call returns `already_initialized`

#### Scenario: Runtime reflects the configured tenant
- **WHEN** `GET /api/admin/runtime` is called for the initialized tenant
- **THEN** the response lists the configured provider, deployment, logical model, route, and key

### Requirement: Session and Console access
The Lite handler SHALL provide `POST /api/admin/session` (local credential login), `DELETE /api/admin/session`, `GET /api/admin/oidc/config` (disabled), and `GET /api/admin/me` returning tenant scopes. The Console SHALL be served as a static SPA with a fallback to `index.html`, while `/api/admin/*` routes to the Admin API.

#### Scenario: Local login establishes a session
- **WHEN** valid local credentials are posted to `/api/admin/session`
- **THEN** a secure HttpOnly session cookie and a CSRF token are returned
- **AND** `/api/admin/me` resolves the tenant scope

#### Scenario: Console is served without Admin API interference
- **WHEN** a non-API path is requested
- **THEN** the Console SPA is returned
- **AND** `/api/admin/*` paths are not captured by the SPA fallback

### Requirement: Main binary composes the Lite profile
`cmd/liteaig/main.go` SHALL accept a `--db` DSN and `--admin-addr`; when a DB is configured it SHALL start the Lite handler on the admin address alongside the readiness server, and drain on termination. Without a DB, it SHALL preserve the current readiness-only behavior.

#### Scenario: Lite profile starts with SQLite
- **WHEN** `--db` and `--admin-addr` are provided
- **THEN** the management plane serves on the admin address and readiness serves on the ready address
- **AND** both shut down gracefully on signal

#### Scenario: Readiness-only mode is preserved
- **WHEN** no `--db` is provided
- **THEN** only the readiness server starts (existing behavior)
