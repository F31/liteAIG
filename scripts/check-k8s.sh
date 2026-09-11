#!/usr/bin/env bash
# Validates the Helm chart ships sane Kubernetes manifests: `helm lint` on the
# chart, `helm template` renders it, and (when kubeconform is installed, e.g.
# in CI) every rendered resource is schema-checked with -strict. Local runs
# without kubeconform still get helm lint + a render smoke.
set -eu

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
chart="${repo_root}/deploy/helm/liteaig"

echo "== helm lint =="
helm lint "${chart}"

echo "== helm template =="
rendered="$(mktemp --suffix=.yaml)"
trap 'rm -f "${rendered}"' EXIT
helm template liteaig "${chart}" > "${rendered}"
echo "rendered $(wc -l < "${rendered}") lines"
resource_count="$(grep -c '^apiVersion:' "${rendered}")"
if [ "${resource_count}" -eq 0 ]; then
  echo "helm template rendered zero Kubernetes resources"
  exit 1
fi
echo "rendered ${resource_count} resources"

if command -v kubeconform >/dev/null 2>&1; then
  echo "== kubeconform (strict) =="
  kubeconform -strict -summary "${rendered}"
else
  echo "kubeconform not installed; skipping schema validation (CI installs it)"
fi

echo "k8s chart checks passed"
