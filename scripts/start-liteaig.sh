#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

MODE="${MODE:-all}"
READY_ADDR="${READY_ADDR:-127.0.0.1:18080}"
ADMIN_ADDR="${ADMIN_ADDR:-127.0.0.1:18081}"
GATEWAY_ADDR="${GATEWAY_ADDR:-127.0.0.1:18082}"
DB_DSN="${DB_DSN:-file:$ROOT_DIR/data/lite-dev.db}"
BIN_PATH="${BIN_PATH:-$ROOT_DIR/bin/liteaig}"

mkdir -p "$(dirname "$BIN_PATH")" "$ROOT_DIR/data"

echo "Building liteaig -> $BIN_PATH"
go build -o "$BIN_PATH" ./cmd/liteaig

echo "Starting LiteAIG"
echo "  mode        : $MODE"
echo "  ready       : http://$READY_ADDR/readyz"
echo "  admin       : http://$ADMIN_ADDR"
echo "  gateway     : http://$GATEWAY_ADDR"
echo "  db          : $DB_DSN"
echo
echo "Press Ctrl+C to stop gracefully."

exec "$BIN_PATH" \
  -mode "$MODE" \
  -ready-addr "$READY_ADDR" \
  -admin-addr "$ADMIN_ADDR" \
  -gateway-addr "$GATEWAY_ADDR" \
  -db "$DB_DSN"
