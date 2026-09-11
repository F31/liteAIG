#!/usr/bin/env bash
#
# restore-liteaig.sh — restore a backup-liteaig.sh archive.
#
# Usage:
#   SQLite:   restore-liteaig.sh --archive path/backup.tar.gz --db /var/lib/liteaig/db.sqlite
#   Postgres: restore-liteaig.sh --archive path/backup.tar.gz --postgres "postgres://user:pass@host:5432/target?sslmode=disable"
#
# SQLite restore copies the archived database and `*.masterkey` sidecar into
# place (0600). Stop the LiteAIG process before restoring over a live database.
# Postgres restore applies the archived dump to the target database with
# `pg_restore` (existing data is left untouched; create a fresh database for a
# clean restore).
set -euo pipefail

usage() {
  sed -n '2,12p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
  exit 1
}

ARCHIVE=""
DB=""
PGDSN=""

while [ $# -gt 0 ]; do
  case "$1" in
    --archive) ARCHIVE="$2"; shift 2 ;;
    --db) DB="$2"; shift 2 ;;
    --postgres) PGDSN="$2"; shift 2 ;;
    *) usage ;;
  esac
done

if [ -z "$ARCHIVE" ] || { [ -z "$DB" ] && [ -z "$PGDSN" ]; }; then
  usage
fi
if [ ! -f "$ARCHIVE" ]; then
  echo "error: archive not found: $ARCHIVE" >&2
  exit 1
fi

read_manifest() {
  local envs
  envs="$(tar xzf "$ARCHIVE" --to-stdout --wildcards "*MANIFEST" 2>/dev/null || true)"
  if [ -z "$envs" ]; then
    echo "error: no MANIFEST in archive; not a backup-liteaig artifact" >&2
    exit 1
  fi
  # shellcheck disable=SC1090
  eval "$(echo "$envs" | sed 's/^/__M_/')"
}

restore_sqlite() {
  command -v tar >/dev/null
  local db="$DB"
  case "$db" in
    file:*) db="${db#file:}" ;;
  esac
  db="${db%%\?*}"
  mkdir -p "$(dirname "$db")"
  tar xzf "$ARCHIVE" --wildcards --transform "s|.*/db.sqlite|$(basename "$db")|" -C "$(dirname "$db")" "*db.sqlite"
  if [ -n "${__M_masterkey_relative:-}" ]; then
    tar xzf "$ARCHIVE" --wildcards --transform "s|.*/db.sqlite.masterkey|$(basename "$db").masterkey|" -C "$(dirname "$db")" "*db.sqlite.masterkey"
    chmod 600 "$db.masterkey"
  fi
  echo "Restored SQLite database: $db"
  if command -v sqlite3 >/dev/null; then
    echo "Integrity: $(sqlite3 "$db" 'PRAGMA integrity_check;')"
  fi
  if [ -z "${__M_masterkey_relative:-}" ]; then
    echo "warning: archive carried no masterkey sidecar. Restore only works if" >&2
    echo "         LITEAIG_MASTER_KEY (base64, 32 bytes) is already set on the target." >&2
  fi
}

restore_postgres() {
  local pg_restore_bin="${PG_RESTORE:-pg_restore}"
  command -v "$pg_restore_bin" >/dev/null 2>&1 || command -v $(basename "$pg_restore_bin") >/dev/null || { echo "error: $pg_restore_bin (or pg_restore) is required (postgresql-client matching the dump)" >&2; exit 1; }
  local dump
  dump="$(mktemp)"
  trap 'rm -f "$dump"' RETURN
  tar xzf "$ARCHIVE" --wildcards -O "*db.dump" > "$dump"
  # Feed the archive over stdin so both host binaries and `docker exec`
  # wrappers work (a container pg_restore cannot open a host file path).
  $pg_restore_bin --no-owner --no-privileges -d "$PGDSN" < "$dump"
  echo "Restored Postgres dump into: ${PGDSN%%\?*}"
}

if [ -n "$DB" ]; then
  read_manifest
  if [ "${__M_mode:-}" != "sqlite" ]; then
    echo "error: archive mode is '${__M_mode:-unknown}', not 'sqlite'" >&2
    exit 1
  fi
  restore_sqlite
else
  read_manifest
  if [ "${__M_mode:-}" != "postgres" ]; then
    echo "error: archive mode is '${__M_mode:-unknown}', not 'postgres'" >&2
    exit 1
  fi
  restore_postgres
fi

echo "Restore complete. Start LiteAIG and confirm /readyz plus a data check (see docs/DR_RUNBOOK.md)."