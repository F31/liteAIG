## 1. Browser E2E Suite

- [x] 1.1 Add `web/console/e2e/*.spec.ts` (Playwright, Chromium) covering the full loop: Setup wizard → virtual key → local login → dashboard → Playground run → Request Explorer detail → config draft publish → versions.
- [x] 1.2 Add `web/console/playwright.config.ts` that launches the built Lite binary (embedded Console + Admin API) against a temp SQLite DB via `webServer`.
- [x] 1.3 Add `npm run test:e2e` (build console + run Playwright) and verify it passes locally.
- [x] 1.4 Fix browser-surfaced bugs: (a) Setup wizard validation always failed because antd `Input` swallowed react-hook-form's `ref` — switched the wizard to `Controller`; (b) CSRF token was lost on SPA full reload, breaking Playground/Projects/Config POSTs — expose the token via `/api/admin/me` and restore it on app mount; (c) add `aria-label`s to Playground inputs for testability/accessibility.

## 2. Lighthouse Accessibility Gate

- [x] 2.1 Verify `lighthouserc.json` (accessibility ≥ 0.9) is runnable against the served dashboard and record the score. — score 0.96, see `evidence/release-closure.md` + `evidence/lighthouse-accessibility.json`.

## 3. Release Regression

- [x] 3.1 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures. — all green, see `evidence/release-closure.md`.
- [x] 3.2 Run `openspec validate --all --strict` and record DoD evidence. — 28 passed, 0 failed; evidence in `evidence/release-closure.md`.
