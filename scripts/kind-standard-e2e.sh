#!/usr/bin/env bash
# Runs a real Kubernetes smoke for the Standard-tier topology: Postgres storage,
# Redis coordinator, Helm install, multi-replica rollout, readiness, and pod
# recovery after one gateway/control process is killed.
set -eu

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cluster="${LITEAIG_KIND_CLUSTER:-liteaig-standard}"
image="${LITEAIG_KIND_IMAGE:-liteaig:kind-standard}"
namespace="${LITEAIG_KIND_NAMESPACE:-liteaig-standard}"
postgres_image="${LITEAIG_KIND_POSTGRES_IMAGE:-postgres:17-alpine}"
redis_image="${LITEAIG_KIND_REDIS_IMAGE:-redis:7-alpine}"
postgres_kind_image="${LITEAIG_KIND_POSTGRES_LOCAL_IMAGE:-liteaig-kind-postgres:17-alpine}"
redis_kind_image="${LITEAIG_KIND_REDIS_LOCAL_IMAGE:-liteaig-kind-redis:7-alpine}"
provider_kind_image="${LITEAIG_KIND_PROVIDER_IMAGE:-liteaig-kind-provider:latest}"

diagnose() {
  kubectl -n "${namespace}" get pods -o wide >/tmp/liteaig-kind-pods.txt 2>&1 || true
  kubectl -n "${namespace}" get events --sort-by=.lastTimestamp >/tmp/liteaig-kind-events.txt 2>&1 || true
  kubectl -n "${namespace}" logs deploy/postgres --tail=100 >/tmp/liteaig-kind-postgres.log 2>&1 || true
  kubectl -n "${namespace}" logs deploy/redis --tail=100 >/tmp/liteaig-kind-redis.log 2>&1 || true
  : >/tmp/liteaig-kind-liteaig.log
  : >/tmp/liteaig-kind-liteaig-previous.log
  for pod in $(kubectl -n "${namespace}" get pods -l app.kubernetes.io/component=gateway -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || true); do
    {
      echo "===== ${pod} (current) ====="
      kubectl -n "${namespace}" logs "${pod}" --tail=200 2>&1 || true
      echo "===== ${pod} (previous) ====="
      kubectl -n "${namespace}" logs "${pod}" --tail=200 --previous 2>&1 || true
    } >>/tmp/liteaig-kind-liteaig.log
  done
  cat /tmp/liteaig-kind-pods.txt || true
  cat /tmp/liteaig-kind-events.txt || true
  cat /tmp/liteaig-kind-liteaig-previous.log || true
  cat /tmp/liteaig-kind-liteaig.log || true
}

cleanup() {
  kind delete cluster --name "${cluster}" >/dev/null 2>&1 || true
}
on_exit() {
  status=$?
  if [ "${status}" -ne 0 ]; then
    diagnose
  fi
  cleanup
  exit "${status}"
}
trap on_exit EXIT

echo "== build image =="
build_dir="$(mktemp -d)"
CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o "${build_dir}/liteaig" "${repo_root}/cmd/liteaig"
cat > "${build_dir}/Dockerfile" <<'DOCKERFILE'
FROM scratch
COPY liteaig /liteaig
USER 65532:65532
ENTRYPOINT ["/liteaig"]
DOCKERFILE
docker build -t "${image}" "${build_dir}"
cat > "${build_dir}/provider.go" <<'GO'
package main

import (
	"fmt"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test-provider" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[{"id":"gpt-4o-mini","object":"model"}]}`)
	})
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test-provider" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"chatcmpl-kind","object":"chat.completion","model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"provider-echo"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`)
	})
	if err := http.ListenAndServe(":8080", mux); err != nil {
		panic(err)
	}
}
GO
CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o "${build_dir}/provider" "${build_dir}/provider.go"
cat > "${build_dir}/Provider.Dockerfile" <<'DOCKERFILE'
FROM scratch
COPY provider /provider
USER 65532:65532
ENTRYPOINT ["/provider"]
DOCKERFILE
docker build -f "${build_dir}/Provider.Dockerfile" -t "${provider_kind_image}" "${build_dir}"

echo "== pull dependency images =="
docker pull --platform linux/amd64 "${postgres_image}"
docker pull --platform linux/amd64 "${redis_image}"
docker build --platform linux/amd64 -t "${postgres_kind_image}" - <<DOCKERFILE
FROM ${postgres_image}
DOCKERFILE
docker build --platform linux/amd64 -t "${redis_kind_image}" - <<DOCKERFILE
FROM ${redis_image}
DOCKERFILE

echo "== create kind cluster =="
kind delete cluster --name "${cluster}" >/dev/null 2>&1 || true
kind create cluster --name "${cluster}" --wait 120s
kind load docker-image "${image}" --name "${cluster}"
kind load docker-image "${postgres_kind_image}" --name "${cluster}"
kind load docker-image "${redis_kind_image}" --name "${cluster}"
kind load docker-image "${provider_kind_image}" --name "${cluster}"

kubectl create namespace "${namespace}"

echo "== install postgres and redis =="
kubectl -n "${namespace}" create secret generic liteaig-postgres \
  --from-literal=POSTGRES_DB=liteaig \
  --from-literal=POSTGRES_USER=liteaig \
  --from-literal=POSTGRES_PASSWORD=liteaig_test

kubectl -n "${namespace}" apply -f - <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: postgres
spec:
  replicas: 1
  selector:
    matchLabels: {app: postgres}
  template:
    metadata:
      labels: {app: postgres}
    spec:
      containers:
        - name: postgres
          image: ${postgres_kind_image}
          imagePullPolicy: IfNotPresent
          envFrom:
            - secretRef: {name: liteaig-postgres}
          ports:
            - containerPort: 5432
          # Postgres finishes initdb a few seconds after the process starts.
          # Without a probe, `rollout status` returns while connections are
          # still refused and the gateway pods race the DB init. pg_isready
          # gates true readiness so the DB is accepting clients first.
          startupProbe:
            exec:
              command: ["pg_isready", "-U", "liteaig", "-d", "liteaig"]
            periodSeconds: 2
            timeoutSeconds: 2
            failureThreshold: 30
          readinessProbe:
            exec:
              command: ["pg_isready", "-U", "liteaig", "-d", "liteaig"]
            periodSeconds: 2
            timeoutSeconds: 2
            failureThreshold: 3
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
spec:
  selector: {app: postgres}
  ports:
    - port: 5432
      targetPort: 5432
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: provider
spec:
  replicas: 1
  selector:
    matchLabels: {app: provider}
  template:
    metadata:
      labels: {app: provider}
    spec:
      containers:
        - name: provider
          image: ${provider_kind_image}
          imagePullPolicy: IfNotPresent
          ports:
            - containerPort: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: provider
spec:
  selector: {app: provider}
  ports:
    - port: 8080
      targetPort: 8080
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis
spec:
  replicas: 1
  selector:
    matchLabels: {app: redis}
  template:
    metadata:
      labels: {app: redis}
    spec:
      containers:
        - name: redis
          image: ${redis_kind_image}
          imagePullPolicy: IfNotPresent
          ports:
            - containerPort: 6379
---
apiVersion: v1
kind: Service
metadata:
  name: redis
spec:
  selector: {app: redis}
  ports:
    - port: 6379
      targetPort: 6379
YAML

kubectl -n "${namespace}" rollout status deployment/postgres --timeout=180s
kubectl -n "${namespace}" rollout status deployment/redis --timeout=180s
kubectl -n "${namespace}" rollout status deployment/provider --timeout=180s

echo "== helm install liteaig standard replicas =="
master_key="${LITEAIG_MASTER_KEY:-MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=}"
helm upgrade --install liteaig "${repo_root}/deploy/helm/liteaig" \
  --namespace "${namespace}" \
  --set-string image.repository="${image%:*}" \
  --set-string image.tag="${image##*:}" \
  --set image.pullPolicy=IfNotPresent \
  --set workloads.gateway.mode=all \
  --set workloads.gateway.replicas=2 \
  --set workloads.gateway.pdb.minAvailable=1 \
  --set workloads.gateway.autoscaling.enabled=false \
  --set-string workloads.gateway.extraArgs[0]="--db=postgres://liteaig:liteaig_test@postgres:5432/liteaig?sslmode=disable" \
  --set-string workloads.gateway.extraArgs[1]="--coordinator=redis://redis:6379/0" \
  --set-string workloads.gateway.extraArgs[2]="--egress-allow-cidrs=10.0.0.0/8" \
  --set workloads.gateway.extraEnv[0].name=LITEAIG_MASTER_KEY \
  --set-string workloads.gateway.extraEnv[0].value="${master_key}"

kubectl -n "${namespace}" wait --for=jsonpath='{.status.phase}'=Running pod -l app.kubernetes.io/component=gateway --timeout=180s

echo "== initialize LiteAIG setup =="
kubectl -n "${namespace}" port-forward deploy/liteaig-liteaig-gateway 18081:8081 >/tmp/liteaig-kind-admin-pf.log 2>&1 &
admin_pf=$!
setup_body='{"username":"admin","adminPassword":"password-123456","tenantName":"Kind","providerName":"Kind Provider","providerType":"openai","providerEndpoint":"http://provider:8080","providerSecret":"sk-test-provider","selectedModel":"gpt-4o-mini"}'
setup_response=""
for i in $(seq 1 30); do
  setup_response="$(curl --noproxy '*' --silent --show-error --write-out '\n%{http_code}' -H 'Content-Type: application/json' -d "${setup_body}" http://127.0.0.1:18081/api/admin/setup || true)"
  setup_status="${setup_response##*$'\n'}"
  if [ "${setup_status}" = "200" ]; then
    break
  fi
  sleep 1
done
if [ "${setup_status:-}" = "401" ]; then
  token="$(kubectl -n "${namespace}" logs deploy/liteaig-liteaig-gateway --tail=100 | awk '/setup bootstrap token/ {print $NF; exit}')"
  setup_response="$(curl --noproxy '*' --silent --show-error --write-out '\n%{http_code}' -H 'Content-Type: application/json' -H "X-Bootstrap-Token: ${token}" -d "${setup_body}" http://127.0.0.1:18081/api/admin/setup || true)"
  setup_status="${setup_response##*$'\n'}"
fi
kill "${admin_pf}" >/dev/null 2>&1 || true
setup_json="${setup_response%$'\n'*}"
if [ "${setup_status:-}" != "200" ]; then
  echo "setup failed with status ${setup_status:-unknown}: ${setup_json}"
  exit 1
fi
virtual_key="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["virtualKey"])' <<EOF
${setup_json}
EOF
)"

kubectl -n "${namespace}" rollout status deployment/liteaig-liteaig-gateway --timeout=240s

ready="$(kubectl -n "${namespace}" get pods -l app.kubernetes.io/component=gateway -o jsonpath='{range .items[*]}{.status.containerStatuses[0].ready}{"\n"}{end}' | grep -c '^true$')"
if [ "${ready}" -lt 2 ]; then
  kubectl -n "${namespace}" get pods -o wide
  kubectl -n "${namespace}" logs deploy/liteaig-liteaig-gateway --tail=100 || true
  echo "expected at least 2 ready liteaig pods, got ${ready}"
  exit 1
fi

echo "== kill one pod and verify recovery =="
victim="$(kubectl -n "${namespace}" get pods -l app.kubernetes.io/component=gateway -o jsonpath='{.items[0].metadata.name}')"
kubectl -n "${namespace}" delete pod "${victim}" --wait=false
kubectl -n "${namespace}" rollout status deployment/liteaig-liteaig-gateway --timeout=240s

echo "== probe /readyz through port-forward =="
kubectl -n "${namespace}" port-forward svc/liteaig-liteaig-gateway 18080:8080 >/tmp/liteaig-kind-pf.log 2>&1 &
pf=$!
trap 'status=$?; kill ${pf} >/dev/null 2>&1 || true; if [ "${status}" -ne 0 ]; then diagnose; fi; cleanup; exit "${status}"' EXIT
for i in $(seq 1 30); do
  if curl --noproxy '*' --fail --silent http://127.0.0.1:18080/readyz >/dev/null; then
    echo "kind standard e2e passed"
    exit 0
  fi
  sleep 1
done
cat /tmp/liteaig-kind-pf.log || true
echo "readyz did not become reachable"
exit 1
