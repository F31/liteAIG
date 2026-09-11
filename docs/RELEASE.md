# Release Checklist

This checklist turns a LiteAIG release into a repeatable operator procedure.
Run it from a clean workspace before creating a `v*` tag.

## 1. Pre-Tag Gates

- Formatting: `test -z "$(gofmt -l .)"`
- Go static checks: `go vet ./...`
- Architecture boundaries: `go run ./cmd/architecture-test`
- Full Go tests: `go test -count=1 ./...`
- Race-sensitive A2A worker smoke:
  `go test -race -count=1 ./internal/app -run 'TestA2APushWorkerTwoProcessDeliversExactlyOnce|TestA2APushWorkerTwoRacingWorkersDeliverExactlyOnce'`
- Go vulnerability scan: `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`
- Console checks: `npm run check` in `web/console`
- Console production dependency audit:
  `NPM_CONFIG_REGISTRY=https://registry.npmjs.org npm audit --omit=dev --audit-level=high` in `web/console`
- Kubernetes manifests: `scripts/check-k8s.sh`
- Container build: `docker build --network=host -t liteaig:release-candidate .`
- Container smoke: run the image with a mounted `/data` directory and probe
  `/api/admin/oidc/config` on the admin listener.

## 2. Release Tag

- Confirm `docs/A2A_TRUSTED_RELEASE.md`, `docs/A2A_CONFORMANCE.md`, and
  `docs/DEPLOYMENT.md` match the intended release scope.
- Before tagging, run a release dry-run from the GitHub Actions UI
  (`workflow_dispatch` on the `release` workflow with `dry_run=true`): it builds
  the image, generates the SPDX SBOM, and uploads the SBOM as an artifact while
  skipping GHCR push and the GitHub Release. Confirm the job succeeds.
- Create an annotated tag: `git tag -a vX.Y.Z -m "LiteAIG vX.Y.Z"`.
- Push the tag: `git push origin vX.Y.Z`.
- The `release.yaml` workflow builds the image, pushes GHCR tags, produces an
  SPDX SBOM, and creates the GitHub Release.

## 3. Post-Release Verification

- Pull the GHCR image by immutable digest and run the same container smoke.
- Verify the GitHub Release contains the SBOM artifact.
- Verify the image runs as non-root (`65532`) and persists SQLite state under
  the mounted `/data` volume.
- For Kubernetes, render the published chart values and run
  `scripts/check-k8s.sh` with kubeconform installed.
- For A2A trusted release validation, exercise:
  - Agent Card discover/review/activate.
  - Durable signed push delivery with `X-LiteAIG-A2A-Push-ID` and signature
    verification.
  - Two-worker or two-process exactly-once delivery over the shared outbox.
- On a fresh tenant, import default alert rules from the Console Alerts page or
  `POST /api/admin/alerts/rules/import-defaults` and confirm repeated imports
  are idempotent.

## 4. Rollback Criteria

Rollback or stop rollout when any of these occur:

- Admin or gateway readiness does not recover within the rollout window.
- A2A push outbox pending/sending rows grow without successful deliveries.
- Push callback failures are dominated by signature, replay, or URL errors.
- `liteaig_a2a_push_outbox_deliveries` reports stalled or increasing failure
  counts after retries should have drained.
- Tenant setup, login, or OIDC endpoints regress during smoke checks.

Rollback steps:

- Direct process: stop the new process, restart the prior binary with the same
  database and master key sidecar.
- Container: redeploy the previous image digest. Do not delete `/data` or rotate
  `LITEAIG_MASTER_KEY` during rollback.
- Kubernetes: roll back the Deployment/Helm release and confirm readiness plus
  push outbox drain.
