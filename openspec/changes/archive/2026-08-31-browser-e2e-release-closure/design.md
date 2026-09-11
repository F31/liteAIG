# browser-e2e-release-closure: Design

## Context

Changes 1–3 produced a runnable Lite management plane with real backend data links and complete Console interactions. The final closure step is proof in a real browser: the actual user journey exercised through the production binary (embedded Console + Admin API on one origin), plus the Lighthouse accessibility gate and a final full regression.

## Goals / Non-Goals

**Goals:**
- Playwright (Chromium) E2E of the full loop: Setup wizard → virtual key → local login → dashboard → Playground → Request Explorer → config publish/versions.
- Lighthouse accessibility ≥ 0.9 for the dashboard.
- Final full-gate regression + `openspec validate --all --strict`.

**Non-Goals:** cross-browser matrix, visual snapshots, CI wiring, browser coverage of every edge case (Go integration suite covers those).

## Decisions

### 1. E2E targets the real embedded binary, self-contained
`playwright.config.ts` declares a `webServer` that deletes the temp SQLite DB and execs the built Lite binary (`e2e/.bin/liteaig-e2e`, `cmd/liteaig`) on a temp SQLite DB, waiting on `http://127.0.0.1:8091/readyz`; `baseURL` is the admin address. `npm run test:e2e` = console build → `go build` of the binary (embedding the fresh `dist`) → `playwright test`. Every run therefore boots a pristine instance — Setup can only run once (`setup.ErrAlreadyInitialized`), so a stale DB makes the loop non-re-runnable. This exercises the actual embed of `web/console/dist`, the SPA fallback, cookie/CSRF session flow, and the Admin API — the exact stack a user hits.

### 2. Bugs surfaced and fixed by the E2E
The browser test exposed two real integration bugs:
- **Setup validation always failed**: antd `Input` swallowed react-hook-form's `ref`, so RHF read empty values and validation failed on every field. Fix: switch the Setup wizard inputs to `Controller` (controlled mode). A regression test (`src/Setup.*.test.tsx`) covers it; the temporary test was replaced by the E2E.
- **CSRF token lost on SPA full reload**: the token lived only in memory, so after `page.goto` (full reload) every POST (Playground/Projects/Config) returned 401. Fix: store the token server-side and expose it via `GET /api/admin/me`; `App.tsx` restores it on mount.
- Also added `aria-label`s to Playground inputs for accessibility/testability.

### 3. Test isolation
vitest excludes `e2e/**` so the Playwright spec does not run under vitest; `npm run test:e2e` builds the console, builds the Go binary, then runs Playwright against the `webServer`-managed instance.

## Risks / Trade-offs

- [E2E env] → headless Chromium; needs the Go toolchain (binary is built by the script) and free ports 8090/8091.
- [Lighthouse] → `lighthouserc.json` asserts `categories:accessibility ≥ 0.9`; the score is recorded as evidence (may require LHCI in CI).

## Migration Plan

1. `web/console/e2e/management-loop.spec.ts` + `playwright.config.ts` + `npm run test:e2e`.
2. Fix browser-surfaced bugs (Setup Controller, CSRF via /me, Playground aria-labels).
3. Lighthouse run + evidence; full gate regression.
