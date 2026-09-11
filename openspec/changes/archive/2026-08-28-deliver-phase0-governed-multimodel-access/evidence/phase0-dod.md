# Phase 0 DoD Evidence — deliver-phase0-governed-multimodel-access

Status: Release Candidate validated on 2026-08-28.

## Execution Environment

- OS: Linux/amd64, Go 1.24.2
- Databases: SQLite (pure-Go) and PostgreSQL 17 container
- Node: v22.23.1, npm 10.9.8
- Console deps: antd 5.27.1, react 19.1.1, vite 7.3.6, vitest 3.2.7
- SDK contract: openai 7.8.0, @anthropic-ai/sdk 0.122.0

## Release Gates (all pass)

| Gate | Command | Result |
|---|---|---|
| Go race tests (incl. PostgreSQL 17) | `LITEAIG_TEST_POSTGRES_DSN=... go test -race ./...` | PASS |
| Vet | `go vet ./...` | PASS |
| Formatting | `test -z "$(gofmt -l .)"` | PASS |
| Architecture boundaries | `go run ./cmd/architecture-test` | PASS |
| Workflow lint | `actionlint .github/workflows/ci.yml` | PASS |
| Single binary build | `go build -o /tmp/liteaig ./cmd/liteaig` | PASS |
| Secret scan | `gitleaks detect --no-git --source .` | PASS |
| Console full check | `npm run check` (prettier, i18n parity, hardcoded strings, vitest, tsc, vite build) | PASS |
| Console vulnerabilities | `npm audit --audit-level=high` | 0 vulnerabilities |
| SDK vulnerabilities | `npm audit --audit-level=high` (tests/contract/sdk) | 0 vulnerabilities |
| OpenSpec strict | `openspec validate --all --strict` | PASS |

## Golden Scenario A

- 10 Virtual Keys, 10 applications, one `default-chat` Logical Model
- First 5 requests routed to OpenAI-compatible, last 5 to Anthropic after snapshot publication
- Client request code unchanged; no Provider Bridge
- 10 request records + 10 usage events persisted and readable by same request_id
- Selected deployment, key, model, tokens, latency, snapshot version, and unavailable cost correlated per request
- PASS (0.04s)

## Phase 0 DoD Items

- Five-minute first call: Scenario A completes well under 5 minutes.
- Playground and Request Explorer agree on the same request_id and decision facts (shared pipeline + accounting context).
- Toggles cannot bypass Draft/Publish (server-side Draft-first config lifecycle).
- Provider contract suite: normal, timeout, 429, 5xx, stream-drop, cancellation pass.
- Stage benchmark: Core Pipeline P99 42ns on local reference hardware, far under the 6ms blocking budget.
- Loaded config continues forwarding when Control Plane repositories are unavailable (request snapshot pinning test).
- Lighthouse Accessibility 0.96 (>= 0.90 gate) via real Chrome on the built Console.
- Scenario A multi-model unified egress passes.

## Known Residual Risks / Reproduction Notes

- Standard-profile PostgreSQL conformance is wired into CI (`LITEAIG_TEST_POSTGRES_DSN`); local runs require a PostgreSQL instance.
- Lighthouse uses a real browser and is enforced in CI via `lighthouse-ci-action`; local verification used a Lighthouse Docker image against the built `dist`.
- Core benchmark numbers reflect the local machine; the blocking SLO target remains <=6ms on the reference environment.
