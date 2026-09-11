# browser-e2e-release-closure

## Why

Changes 1–3 delivered a runnable Lite management plane with real data links and complete Console interactions. The final closure step is a **browser-level end-to-end test** that proves the actual user journey works through the real HTTP stack and the real SPA: Setup wizard → virtual key → Playground request → Request Explorer → Config publish, plus an accessibility gate (Lighthouse ≥ 90) and a full regression run. This catches integration issues that unit/component tests cannot (routing, CSRF/cookies, proxy-less same-origin serving, SPA fallback, localization).

## What Changes

- **Playwright E2E**: a browser test that boots the built Lite binary (serving the embedded Console + Admin API on one address), runs the Setup wizard, logs in, runs a Playground request, opens the Request Explorer detail, publishes a config draft, and asserts each surface renders.
- **Test harness**: a `web/console/e2e/` suite with a config that points at a live Lite instance, plus scripts to build the console and start/stop the backend for the run.
- **Accessibility gate**: `lighthouserc.json` asserts `categories:accessibility ≥ 0.9`; verify the report can be produced for the running app.
- **Release regression**: run the full gate set and record DoD evidence.

## Capabilities

### New Capabilities
- `browser-e2e-release-closure`: browser-level E2E for the full management loop + Lighthouse accessibility gate.

### Modified Capabilities
- `runnable-management-plane` / `console-interaction-completeness`: end-to-end journey is proven in a real browser.

## Impact

- **Tests**: `web/console/e2e/*.spec.ts` (Playwright) + harness scripts; optional `npm run test:e2e`.
- **Docs/CI**: evidence file records browser E2E + Lighthouse + gate results.
- **Dependencies**: Playwright as a dev dependency of the Console (test-only).
- **Non-Goals**: full CI pipeline wiring, cross-browser matrix beyond Chromium, visual snapshots.
