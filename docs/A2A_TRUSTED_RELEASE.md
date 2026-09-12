# A2A Trusted Release Notes

This document captures the current A2A trusted-release contract and the risks
that remain outside the in-repo conformance suite.

## Covered In Repo

- Outbound `message/send` JSON-RPC envelope shape is covered by `internal/connectors/agent/a2a/connector_test.go` (`TestA2AConnectorSendsConformantEnvelope`).
- Inbound JSON-RPC validation is covered by `internal/gateway/server/server_test.go` (`TestA2AStrictJSONRPCValidation`). The `/a2a` surface serves both wire profiles: `message/send` (0.3-style parts) and `SendMessage` (A2A 1.0 protobuf-JSON, `ROLE_*` enums and bare text parts), selected by method.
- Official SDK interop is verified against the latest `a2a-sdk` (tested locally against `1.1.2`): `internal/app/a2a_sdk_conformance_test.go` + `tests/conformance/a2a/client.py` drive a real gateway with the SDK's `ClientFactory` over both `SendMessage` and `SendStreamingMessage`, asserting the SDK-parsed replies carry the relayed text. Runbook in `docs/A2A_CONFORMANCE.md`; `.github/workflows/conformance.yaml` runs it on networked runners.
- Outbound and inbound streaming profile behavior is covered by connector and gateway tests in `internal/connectors/agent/a2a` and `internal/gateway/server`; `internal/app/a2a_streaming_e2e_test.go` (`TestA2AInboundStreamingRelaysIncrementalDeltas`) proves an SSE-speaking peer is relayed to the inbound client as one `type: message` event per remote delta plus a single `completed`, with exactly one outbound POST.
- Agent Card discovery, signature verification, operator review, activation, and material-change fail-closed behavior are covered by `internal/app/a2a_card_lifecycle_e2e_test.go`.
- Durable task idempotency, hop/call limits, `pending -> running` CAS, stale-running recovery, durable signed push callback retry, and restart replay are covered by SQL/app e2e tests.
- Push callback HMAC signing and verification helpers, including delivery-id binding, are covered by `internal/access/protocol/a2a/pushsig_test.go`.
- The Lite/Standard push outbox worker is guarded by the shared `coordination_leases` singleton scope `platform:a2a_push_outbox`, each due row is atomically claimed with `pending -> sending` before delivery, and callback URLs, bearer tokens, plus callback payloads are encrypted at rest in the outbox.
- `internal/app/a2a_push_worker_concurrency_test.go` runs two racing singleton workers over one shared SQL database and proves every queued callback is delivered exactly once (no duplicate delivery ids), approximating the Standard-tier multi-process smoke inside the repo.
- `internal/app/a2a_two_process_smoke_test.go` is the true two-OS-process black-box smoke: two real `liteaig` binaries race one SQLite file (singleton lease + outbox atomic claim) and deliver every queued callback exactly once, with clean SIGTERM drain exit.
- Container delivery is validated end-to-end: `Dockerfile` (single self-contained binary, distroless nonroot, SQLite under `/data`) is built and smoke-probed in CI (`ci.yml` `image` job), published tag-triggered to GHCR with an SPDX SBOM (`release.yaml`), and expressed as a Helm chart in `deploy/helm/liteaig`. See `docs/DEPLOYMENT.md`.
- The release/upgrade operator path is documented in `docs/RELEASE.md` and `docs/UPGRADE_DRILL.md`, including pre-tag gates, post-release verification, rollback criteria, master-key preservation, and A2A push outbox checks.
- `GET /api/admin/federation` merges the compiled snapshot with the lifecycle store, so discovered candidates and operator-reviewed state are visible through the admin surface; `GET /api/admin/federation/push-outbox?status=…` lists the tenant's most recent sanitized callback deliveries (delivery id, task id, status, attempts, max attempts, last error, next attempt/updated timestamps) filtered by pending/sending/delivered/failed for operator drill-down; callback URLs, bearer tokens, and payload bodies are never selected.
- `GET /metrics` exposes low-cardinality A2A push outbox gauges (`liteaig_a2a_push_outbox_deliveries` and earliest pending retry timestamp) without tenant/task/callback labels.
- The Console Agent Card review UX is covered by `web/console/e2e/agent-card-review.spec.ts`: it signs a card with a local `card-server.mjs`, runs discover → verify → review → activate in the browser, and asserts the candidate row resolves to `active` with a verified anchor.

## Release Boundary

- Supported A2A methods: `message/send` (0.3 wire), `SendMessage` (A2A 1.0 blocking wire), and `SendStreamingMessage` (A2A 1.0 JSON-RPC-over-SSE wire, what the official SDK v1.x streaming path sends).
- Supported content profile: text parts for relay; images are accepted on OpenAI/Anthropic compatibility surfaces but not advertised as an A2A relay guarantee.
- Supported streaming profiles: LiteAIG SSE frames documented in `docs/A2A_STREAMING.md` for `message/send` callers, plus A2A 1.0 JSON-RPC-over-SSE frames for `SendStreamingMessage`, asserted through the official SDK's streaming parser.
- Supported push profile: `pushNotificationConfig.url/token` on non-streaming completed results, delivered through a durable outbox when Lite SQL storage is wired.
- Trust model: signed Agent Card discovery creates only a candidate; operator review with verified anchor and explicit project/capability grants is required before inbound federated calls are admitted.

## Residual Risks

- Official third-party A2A SDK/interop conformance is provisioned as a networked
  runner (`.github/workflows/conformance.yaml` + `docs/A2A_CONFORMANCE.md`) and
  was verified locally against `a2a-sdk==1.1.2` for both blocking and streaming
  sends. Each GitHub-hosted PR/push re-verifies the latest `a2a-sdk`; the gate
  env `LITEAIG_A2A_SDK_DIR` keeps offline CI green. A future SDK breaking the
  `SendMessage`/`SendStreamingMessage`/`message/send` surfaces is caught by that
  job.
- Kubernetes manifests rendered from the Helm chart (`deploy/helm/liteaig`) are
  covered by `scripts/check-k8s.sh` and the `ci.yml` `k8s` job: Helm lint,
  Helm template, and strict kubeconform schema validation. The `image` CI job
  and `release.yaml` validate the container image itself.
- The split-plane topology (multiple `gateway` replicas sharing Postgres +
  Redis coordination) is exercised by chaos tests and the `k8s-kind-standard`
  multi-replica CI job, including convergence, pod replacement, and readiness.
- Tenant webhook notification desired state is covered by
  `GET|PUT|DELETE /api/admin/alerts/notifications`; updates hot-reload the
  current process. An explicit startup `--webhook-url`/`LiteOptions.WebhookURL`
  overrides the persisted value on restart.
- Push delivery drill-down (`GET /api/admin/federation/push-outbox?status=…`)
  enumerates sanitized rows for every state (pending/sending/delivered/failed,
  empty selects all), newest-first with a bounded limit; callback URLs, bearer
  tokens, and payload bodies are never selected.
