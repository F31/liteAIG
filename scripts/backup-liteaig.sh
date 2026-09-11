#!/usr/bin/env bash
#
# backup-liteaig.sh — consistent LiteAIG backup (SQLite file + masterkey sidecar,
# or Standard-tier Postgres SQL dump).
#
# Usage:
#   SQLite:    backup-liteaig.sh --db /var/lib/liteaig/db.sqlite [--out DIR]
#   Postgres:  backup-liteaig.sh --postgres "postgres://user:pass@host:5432/db?sslmode=disable" [--out DIR]
#
# The SQLite backup uses `sqlite3 .backup`, which is safe against a live writer
# (it snapshots the database page file), so no downtime is required. The
# `<path>.masterkey` sidecar must be captured together with the database; it is
# required to decrypt secrets after restore. In Postgres mode the backup is a
# plain pg_dump archive; `LITEAIG_MASTER_KEY` (or the secret provider) remains
# operator-managed and must be preserved separately.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

usage() {
  sed -n '2,14p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
  exit 1
}

DB=""
PGDSN=""
OUT_DIR="${OUT_DIR:-backups}"

while [ $# -gt 0 ]; do
  case "$1" in
    --db) DB="$2"; shift 2 ;;
    --postgres) PGDSN="$2"; shift 2 ;;
    --out) OUT_DIR="$2"; shift 2 ;;
    *) usage ;;
  esac
done

if [ -z "$DB" ] && [ -z "$PGDSN" ]; then
  usage
fi
if [ -n "$DB" ] && [ -n "$PGDSN" ]; then
  echo "error: choose --db (SQLite) OR --postgres, not both" >&2
  exit 1
fi

TIME_STAMP="$(date +%Y%m%d%H%M%S)"
BACKUP_DIR="$OUT_DIR/liteaig-$TIME_STAMP"
mkdir -p "$BACKUP_DIR"

backup_sqlite() {
  command -v sqlite3 >/dev/null || { echo "error: sqlite3 CLI is required (apt install sqlite3)" >&2; exit 1; }
  local db="$DB"
  case "$db" in
    file:*) db="${db#file:}" ;;
  esac
  # Strip query parameters (e.g. ?cache=shared) used by DSN strings.
  db="${db%%\?*}"
  if [ ! -f "$db" ]; then
    echo "error: SQLite database not found: $db" >&2
    exit 1
  fi
  local staging="$BACKUP_DIR/db.sqlite"
  sqlite3 "$db" ".backup '$staging'"
  local integrity
  integrity="$(sqlite3 "$staging" 'PRAGMA integrity_check;')"
  if [ "$integrity" != "ok" ]; then
    echo "error: integrity check failed on backup: $integrity" >&2
    exit 1
  fi
  local manifest_keys="mode=sqlite"$'\n'"db_relative=db.sqlite"
  if [ -f "$db.masterkey" ]; then
    cp "$db.masterkey" "$BACKUP_DIR/db.sqlite.masterkey"
    chmod 600 "$BACKUP_DIR/db.sqlite.masterkey"
    manifest_keys+=$'\n'"masterkey_relative=db.sqlite.masterkey"
  fi
  echo -e "$manifest_keys"$'\n'"integrity=$integrity" > "$BACKUP_DIR/MANIFEST"
}

backup_postgres() {
  local pg_dump_bin="${PG_DUMP:-pg_dump}"
  command -v "$pg_dump_bin" >/dev/null 2>&1 || command -v $(basename "$pg_dump_bin") >/dev/null || { echo "error: $pg_dump_bin (or pg_dump) is required (postgresql-client matching the server major)" >&2; exit 1; }
  # Stream to stdout so host binaries and `docker exec` wrappers both work
  # (a container pg_dump cannot open a host `-f` path).
  $pg_dump_bin "$PGDSN" -Fc > "$BACKUP_DIR/db.dump"
  echo "mode=postgres"$'\n'"dump_relative=db.dump" > "$BACKUP_DIR/MANIFEST"
}

pack() {
  ( cd "$OUT_DIR" && tar czf "liteaig-$TIME_STAMP.tar.gz" "liteaig-$TIME_STAMP" )
  rm -rf "$BACKUP_DIR"
}

if [ -n "$DB" ]; then
  backup_sqlite
else
  backup_postgres
fi
pack

echo "Backup created: $OUT_DIR/liteaig-$TIME_STAMP.tar.gz"
echo "MANIFEST:"
sed 's/^/  /' <(tar xzf "$OUT_DIR/liteaig-$TIME_STAMP.tar.gz" -O "liteaig-$TIME_STAMP/MANIFEST" 2>/dev/null || true)
echo "Restore with: scripts/restore-liteaig.sh --archive $OUT_DIR/liteaig-$TIME_STAMP.tar.gz <target...>"