# Upgrade And Migration Drill

Use this drill before each release candidate and after any migration that touches
tenant state, credentials, coordination leases, or the A2A push outbox.

## 1. Assets To Preserve

- SQLite deployments: preserve the database file and its `*.masterkey` sidecar.
- Container deployments: preserve the mounted `/data` volume.
- Standard deployments: preserve Postgres, Redis coordination, and the configured
  secret provider or `LITEAIG_MASTER_KEY`.
- A2A deployments: preserve `a2a_tasks`, `a2a_push_outbox`, relationship rows,
  cached Agent Cards, and federation anchors.

Never rotate the master key as part of an application upgrade. Rotation must be
a separate, reversible key-management procedure.

## 2. SQLite Direct-Process Drill

1. Stop the current process with SIGTERM and wait for a clean exit.
2. Copy the SQLite database and `*.masterkey` sidecar to a backup location.
3. Start the new binary with the same `--db` path and addresses.
4. Confirm `/healthz` and `/readyz` recover.
5. Open the Console and verify setup/login, OIDC config, federation overview,
   and alert rules load.
6. Exercise one A2A push callback and verify the outbox drains.
7. If rollback is needed, stop the new binary and restart the previous binary
   against the same files. Do not restore an older DB unless the migration is
   explicitly documented as backward-incompatible.

## 3. Container Drill

1. Pull the candidate image by digest.
2. Start it with the existing `/data` mount and the same CLI flags/env vars.
3. Confirm the process runs as non-root (`65532`) and can write SQLite state.
4. Probe the admin listener, then run the same A2A/federation/alert checks as
   the direct-process drill.
5. Roll back by redeploying the previous image digest with the same `/data`
   mount and master key material.

## 4. Standard-Tier Drill

1. Confirm Postgres and Redis backups are fresh.
2. Deploy one upgraded control-plane process first.
3. Confirm migrations complete and admin APIs serve normally.
4. Deploy upgraded gateway replicas gradually.
5. Watch coordination leases and `a2a_push_outbox` state transitions:
   `pending -> sending -> delivered`.
6. Confirm no row remains in `sending` past the stale-recovery window except
   during active delivery.
7. Roll back replicas to the previous image/binary if readiness, login, or A2A
   delivery regresses. Keep the migrated database in place unless a release note
   explicitly instructs otherwise.

## 5. Data Checks

- Alert rules: `GET /api/admin/alerts/rules` returns the expected tenant rules.
- Notification target: `GET /api/admin/alerts/notifications` returns the desired
  webhook state. Runtime changes hot-reload in the current process; if
  `--webhook-url` is set, it overrides the persisted value at the next startup.
- Federation: `GET /api/admin/federation` includes active relationships and
  discovered-but-not-activated Agent Card candidates.
- Push outbox drill-down:
  `GET /api/admin/federation/push-outbox?status=failed` returns redacted payloads
  and secrets only; the same endpoint accepts pending/sending/delivered for a
  full per-delivery view across every outbox state.
- Metrics: `/metrics` includes `liteaig_a2a_push_outbox_deliveries` gauges.
- Logs: no repeated signature, replay, or master-key decrypt errors.

## 6. Migration Failure Handling

- If startup fails before serving traffic, keep the failed process down and
  inspect migration errors before retrying.
- If a migration partially completed, do not manually edit tables. Take a fresh
  copy of the database and reproduce on the copy first.
- If encrypted payload or URL columns cannot decrypt, verify the process is using
  the same `LITEAIG_MASTER_KEY` or SQLite masterkey sidecar as the previous
  deployment.
- If Redis coordination is unavailable, use a single gateway replica until Redis
  recovers; do not run multiple SQLite writers.
