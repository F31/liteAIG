# browser-e2e-release-closure Specification

## Purpose
Browser-level proof that the full Lite management journey works through the real production stack (embedded Console + Admin API), plus the accessibility gate and the release regression that close a change.

## Requirements

### Requirement: Browser E2E of the full management loop
The Console SHALL provide a Playwright browser E2E that drives the real application against a live Lite instance: the Setup wizard SHALL create the initial tenant, login SHALL establish a session, the Playground SHALL run a governed request, the Request Explorer SHALL show the request, and a config draft SHALL be publishable. Each step SHALL assert visible UI state.

#### Scenario: Setup → key → playground → request → config publish
- **WHEN** a fresh Lite instance serves the Console
- **THEN** the Setup wizard completes and shows a virtual key
- **AND** a local login establishes a session and redirects to the dashboard
- **AND** a Playground run returns an output with usage
- **AND** the Request Explorer shows the created request
- **AND** a config draft can be published and appears in versions

### Requirement: Lighthouse accessibility gate
The Console SHALL maintain a Lighthouse accessibility score of at least 0.9 for the dashboard, enforced by a runnable CI assertion.

#### Scenario: Accessibility assertion passes
- **WHEN** Lighthouse audits the dashboard URL
- **THEN** the accessibility category score is ≥ 0.9

### Requirement: Release regression
The final gate set SHALL run clean (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and `openspec validate --all --strict` SHALL pass for all changes.

#### Scenario: Full regression
- **WHEN** all gates run
- **THEN** only the documented pre-existing chaos shared-DB flake (passing standalone) may fail
- **AND** every OpenSpec change validates
