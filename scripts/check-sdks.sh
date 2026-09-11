#!/usr/bin/env bash
set -eu

# Report the exact failing line in CI logs (the Actions log download needs
# admin rights, so failures must be self-describing).
on_error() {
  echo "check-sdks.sh: FAILED at line ${1}: ${2}" >&2
}
trap 'on_error "$LINENO" "$BASH_COMMAND"' ERR

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

cd "$ROOT"

go test ./pkg/liteaig

node --test packages/liteaig-js/test.mjs
npm pack --dry-run --json ./packages/liteaig-js >/tmp/liteaig-js-pack.json

(cd packages/liteaig-python && python -m unittest discover -s tests)
python -m py_compile packages/liteaig-python/liteaig/__init__.py packages/liteaig-python/liteaig/client.py

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT

python -m venv --system-site-packages "$tmpdir/venv"
"$tmpdir/venv/bin/python" -m pip install --quiet --upgrade setuptools wheel
"$tmpdir/venv/bin/python" -m pip install --no-build-isolation --no-deps ./packages/liteaig-python
"$tmpdir/venv/bin/python" -c "from liteaig import Client, DEFAULT_API_VERSION; assert DEFAULT_API_VERSION == '2026-09-09'; assert Client"
