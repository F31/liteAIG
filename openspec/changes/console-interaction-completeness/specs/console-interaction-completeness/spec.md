# console-interaction-completeness Specification

## ADDED Requirements

### Requirement: Dashboard aggregate endpoint
`GET /api/admin/dashboard` SHALL return server-aggregated metrics for the tenant: request count, total input/output tokens, average latency, and the active config version. Aggregation SHALL happen on the server; the browser SHALL NOT scan the usage ledger.

#### Scenario: Dashboard returns live aggregates
- **WHEN** a tenant has recorded requests
- **THEN** `GET /api/admin/dashboard` returns request count, token totals, average latency, and active config version
- **AND** the values are computed from the accounting ledger and config versions

### Requirement: Config draft list
`GET /api/admin/config/drafts` SHALL list the tenant's config drafts (id, status, base version, revision, updated at) so the Config page can select a draft instead of typing an id.

#### Scenario: Draft list renders
- **WHEN** a tenant has drafts
- **THEN** `GET /api/admin/config/drafts` returns each draft's id, status, base version, revision, and updated timestamp

### Requirement: Console dev proxy and one-shot startup
The Console dev server SHALL proxy `/api/admin` to the Lite admin address, and a `make dev` target SHALL build/launch both the backend and the Console dev server together.

#### Scenario: Dev proxy forwards admin API
- **WHEN** `npm run dev` runs with `LITEAIG_ADMIN_ADDR` set
- **THEN** requests to `/api/admin/*` are proxied to that backend address

#### Scenario: One-shot local startup
- **WHEN** `make dev` runs
- **THEN** the Lite backend and the Console dev server start together

### Requirement: Project creation from the Console
The Resources page SHALL provide a create-project form that calls `POST /api/admin/projects`, and the projects table SHALL show name, status, residency enforcement, and allowed regions.

#### Scenario: Create project from the page
- **WHEN** a user submits the create-project form
- **THEN** `POST /api/admin/projects` creates the project
- **AND** the projects table refreshes to include it

### Requirement: Config draft editor with safe diff
The Config page SHALL allow selecting a draft, viewing its semantic diff against the latest version, editing the tenant config JSON, saving the draft (with revision conflict handling), publishing, rolling back, and listing versions. Sensitive values SHALL be masked in diff/editor feedback (§37.45).

#### Scenario: Edit and save a draft
- **WHEN** a draft is selected and its JSON edited and saved
- **THEN** `PUT /api/admin/config/drafts/{id}` persists the new revision
- **AND** a stale revision returns `REVISION_CONFLICT`

#### Scenario: Publish a draft
- **WHEN** a validated draft is published
- **THEN** `POST /api/admin/config/drafts/{id}/publish` activates the tenant snapshot
- **AND** the versions list updates
