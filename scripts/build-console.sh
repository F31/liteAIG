#!/usr/bin/env bash
# Builds the Console dist assets required by the Go `//go:embed` of
# web/console/dist. dist/ is intentionally not tracked, so every CI job and the
# container image must build it before compiling the gateway binary.
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

if [ -d "$ROOT/web/console/dist" ] && [ -n "$(ls -A "$ROOT/web/console/dist" 2>/dev/null)" ]; then
  echo "console dist already present; skipping build"
  exit 0
fi

echo "== build console dist =="
if ! command -v npm >/dev/null 2>&1; then
  echo "error: npm is required to build the console" >&2
  exit 1
fi
cd "$ROOT/web/console"
npm ci
npm run build
echo "console dist built"
