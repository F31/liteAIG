# LiteAIG Backup / Restore and DR Runbook

This runbook pairs with `scripts/backup-liteaig.sh` and
`scripts/restore-liteaig.sh`. It walks a Safe-Tier operator through taking a
consistent backup, simulating state loss, restoring, and verifying the
recovered control plane.

## Objectives And RPO / RTO

| Term | Value | Notes |
|---|---|---|
| SQLite RPO | 0 (hot `.backup` snapshot) | `sqlite3 .backup` is safe against a live writer; no downtime for the backup itself. |
| SQLite RTO | minutes | copy files back + single-process restart. |
| Postgres RPO | last successful `pg_dump` | schedule per your retention target (e.g. hourly). |
| Postgres RTO | minutes | `pg_restore` into a fresh database. |

Delegated, ephemeral state needs no backup: Redis coordination leases,
budget reservations, and `runtime` snapshots are rebuilt on startup. The
`LITEAIG_MASTER_KEY` (Postgres tier) or the `*.masterkey` sidecar (SQLite
tier) is the only piece that cannot be reconstructed — losing it destroys
all encrypted secrets (provider credentials, API keys, OIDC tokens).

## Assets To Preserve

- SQLite: the database file **and** its `*.masterkey` sidecar
  (`<db>.masterkey`, base64, `0600`).
- Postgres: a matching-major `pg_dump` custom-format archive, plus the
  `LITEAIG_MASTER_KEY` value (keep it out of the dump and out of the repo).
- Coordination (Redis) is rebuildable; reserve nothing.

## 1. Take A Backup

SQLite (non-docker host, `sqlite3` required):

```bash
scripts/backup-liteaig.sh --db /var/lib/liteaig/db.sqlite --out /backups
```

Postgres (the `pg_dump` client major must match the server major; override
with `PG_DUMP` when the matching client is inside a container):

```bash
scripts/backup-liteaig.sh \
  --postgres "postgres://user:pass@db:5432/liteaig?sslmode=disable" \
  --out /backups

# Containerized matching client example:
PG_DUMP="docker exec -i litepg pg_dump" \
  scripts/backup-liteaig.sh --postgres "postgres://…@127.0.0.1:5432/liteaig" --out /backups
```

Result: `/backups/liteaig-<ts>.tar.gz` containing the database plus a
`MANIFEST` (mode, member paths, SQLite integrity result). Never encrypt the
SQLite sidecar differently than the database; archive them together.

## 2. Simulated Loss Drill (SQLite)

1. Stop LiteAIG: `kill <pid>` (SIGTERM), wait for a clean exit.
2. Simulate loss: `rm /var/lib/liteaig/db.sqlite /var/lib/liteaig/db.sqlite.masterkey`.
3. Restore:
   ```bash
   scripts/restore-liteaig.sh --archive /backups/liteaig-<ts>.tar.gz \
     --db /var/lib/liteaig/db.sqlite
   ```
4. Start LiteAIG with the same flags/addresses it used before.
5. Verify (below).
6. Roll forward: leave the restored state in place; do not restore an older
   archive unless the failing release is documented as backward-incompatible.

## 3. Simulated Loss Drill (Postgres)

1. Create a fresh target database (owner must match control-plane role):
   ```bash
   psql -U liteaig -d postgres -c "CREATE DATABASE liteaig_restore OWNER liteaig;"
   ```
2. Restore:
   ```bash
   scripts/restore-liteaig.sh --archive /backups/liteaig-<ts>.tar.gz \
     --postgres "postgres://user:pass@db:5432/liteaig_restore?sslmode=disable"
   ```
3. Point LiteAIG at the restored database, export the same
   `LITEAIG_MASTER_KEY`, start, and verify.
4. To fully simulate loss, you may drop the original database first; restore
   always targets an existing (possibly empty) database and never truncates.

## 4. Verification Checklist

Automated read-only drill (safe for cron/CI):

```bash
go run ./cmd/drdrill --region region-a --tenant tenant-ref \
  --budget-mode global_soft --lkg-root /var/lib/liteaig/lkg --out /tmp/liteaig-dr.json
```

For long-running cron archives, keep dated JSON reports in a directory with a
retention window (default keeps the newest 30):

```bash
go run ./cmd/drdrill --region region-a --tenant tenant-ref \
  --lkg-root /var/lib/liteaig/lkg \
  --archive-dir /var/lib/liteaig/dr-reports --archive-keep 30
```

Archived files are named `drdrill-<region>-<tenant>-<UTC-timestamp>.json`;
the same JSON is also streamed to stdout for monitoring to consume.

Without `--lkg-root`, the command runs the deterministic checklist only. With
`--lkg-root`, readiness also requires a valid
`regions/<region>/<tenant>/active.bundle` or `previous.bundle`.

- `/healthz` returns 200 on the ready listener.
- `/readyz` returns 200 (a published Runtime must exist).
- Setup/login works: `GET /api/admin/audit` returns the pre-backup events.
- Secrets decrypt: open Resources → Credentials; or `GET /api/admin/keys`
  returns existing virtual keys (their `fingerprint` resolves).
- Alert rules: `GET /api/admin/alerts/rules` returns the expected rules.
- Notification target: `GET /api/admin/alerts/notifications`.
- Federation: `GET /api/admin/federation` lists active relationships.
- A2A push outbox: `GET /api/admin/federation/push-outbox?status=failed`
  still redacts payloads and secrets.
- SQLite drill only: `sqlite3 <db> 'PRAGMA integrity_check;'` → `ok`.

## 5. Scheduling

- SQLite: cron/systemd timer running `backup-liteaig.sh` hourly is
  sufficient for typical single-tenant workloads (the backup is online-safe).
- Postgres: pair `pg_dump` with your existing cluster backup policy; the
  archive keeps a tidy single-file artifact per run.
- Test restore at least monthly and after every migration that touches
  tenant state, credentials, coordination leases, or the A2A push outbox
  (see `docs/UPGRADE_DRILL.md`).

## 6. Failure Handling

- Restore reports an integrity mismatch → do not start the control plane on
  it. Re-run from the newest intact archive; reset only as a last resort.
- Archive carries no `*.masterkey` → restore still completes; startup will
  decrypt nothing unless `LITEAIG_MASTER_KEY` (base64, 32 bytes) is set and
  matches the original deployment.
- `pg_dump`/`pg_restore` version mismatch → install a client matching the
  server major, or override via `PG_DUMP` / `PG_RESTORE` (e.g. `docker exec`).
