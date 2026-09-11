## 1. Lite Composition Root

- [x] 1.1 Add `internal/app` Lite composition: open SQLite, apply migrations, seed active key pepper, construct sqlite repositories (tenancy, bootstrap, config, apikey, accounting, alert, localcredential) and domain services (config.Service, apikey.Service, alert.Service, setup bootstrap + wizard).
- [x] 1.2 Assemble the Admin HTTP handler: `adminapi.Server` (backend + authorizer), `SessionEndpoints` (login/logout/oidc-config), and Console static serving with SPA fallback; return a single `http.Handler`.
- [x] 1.3 Add a real clock, an Argon2id hasher/verifier wiring, and a static key-pepper secret provider seeded at startup.

## 2. Setup Wizard Closure

- [x] 2.1 Implement a Lite `setup.ProviderSetup` (mock provider): Register/Test/DiscoverModels/ConfigureDefault that builds and publishes a tenant config (provider, credential, deployment, route policy, logical model) so the runtime snapshot activates.
- [x] 2.2 Wire `setup.Wizard` (bootstrap + provider setup + apikey.KeyCreator + FirstCallRunner) behind the `WizardSetupAdapter` so `POST /api/admin/setup` returns virtualKey/sdkExample/requestId/tenantId/projectId.
- [x] 2.3 Implement a mock first-call / playground pipeline so Setup's first call and `POST /api/admin/playground` produce a request record (source=playground) persisted via the accounting repository.

## 3. Main Binary Wiring

- [x] 3.1 Extend `cmd/liteaig/main.go` with `--db` (SQLite DSN) and `--admin-addr` flags; when a DB is configured, start the Lite handler on the admin address alongside readiness and drain both on signal; preserve readiness-only behavior when no DB is set.
- [x] 3.2 Add a Lite composition integration test (Setup → session login → runtime query → console served) and keep existing `main_test` green.

## 4. Release Gates

- [x] 4.1 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures.
- [x] 4.2 Run `openspec validate --all --strict` and record DoD evidence for the Lite management plane.
