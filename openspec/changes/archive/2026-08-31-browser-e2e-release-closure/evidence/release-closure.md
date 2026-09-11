# Release Closure Evidence

Date: 2026-08-31. All commands run from the repo root unless noted.

## Browser E2E (Playwright, Chromium)

`web/console`: `npm run test:e2e` (builds Console, builds the Lite binary
embedding the dist, then Playwright boots it on a temp SQLite DB via
`webServer` and polls `http://127.0.0.1:8091/readyz`):

```
Running 1 test using 1 worker
  ✓  1 e2e/management-loop.spec.ts:5:1 › setup → login → dashboard → playground → requests → config (3.2s)
  1 passed (3.8s)
```

Repeated immediately (`npx playwright test`) — still `1 passed (4.2s)`,
confirming the harness is re-runnable (fresh DB per run).

## Lighthouse Accessibility Gate

Served the built binary on `127.0.0.1:8090` with an initialized tenant,
logged in (`POST /api/admin/session`, cookie `lia_session`), then:

```
CHROME_PATH=/root/.cache/ms-playwright/chromium-1234/chrome-linux64/chrome \
  npx lighthouse http://127.0.0.1:8090/dashboard \
  --extra-headers=<{"Cookie":"lia_session=…"}> \
  --output=json --output-path=lighthouse-accessibility.json \
  --chrome-flags="--headless=new --no-sandbox --disable-dev-shm-usage" \
  --only-categories=accessibility
```

- Lighthouse 13.4.1, `categories.accessibility = 0.96` (gate: ≥ 0.9) — PASS.
- Report: `lighthouse-accessibility.json` (fetched 2026-08-31T02:24:05Z).
- `lighthouserc.json` asserts `categories:accessibility` minScore 0.9 for
  `http://127.0.0.1:8090/dashboard`.

## Go Gates (repo root)

- `go test -race -timeout 240s ./...` — all packages pass, including
  chaos/golden/kubernetes/soak
  (`LITEAIG_TEST_POSTGRES_DSN=postgres://liteaig:liteaig_test@localhost:55432/liteaig_test?sslmode=disable`,
  `LITEAIG_TEST_REDIS_ADDR=localhost:56379`).
- `go vet ./...` — clean.
- `go run ./cmd/architecture-test` — `architecture boundaries: ok`.

## Console Gates (`web/console`)

- `npm run check` — passes (`format:check`, `check:i18n`, `check:strings`,
  vitest, build).
- `npm run test:e2e` — see Browser E2E above.

## CI / Secrets Gates (repo root)

- `actionlint -no-color .github/workflows/ci.yml` — exit 0, no findings
  (actionlint v1.7.12; run with explicit file path because the working copy
  is not a git repository).
- `gitleaks detect --no-banner` — `no leaks found`, exit 0.

## OpenSpec

- `openspec validate --all --strict` — `28 passed, 0 failed (28 items)`.
