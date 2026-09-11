# Phase 4 — Semantic Cache: DoD Evidence

Status: Complete (11/11 tasks)

This change delivers a tenant-scoped Semantic Cache (`Prompt → Embedding →
tenant-scoped vector search → threshold → candidate → current Output
Guardrail → return`) reusing the Phase 2 Exact Cache contracts and the
"cache hits still run Output Guardrail and Accounting" invariant.

## Capabilities Delivered

### Semantic Store (`internal/cache/semantic.go`)
- `SemanticStore` contract (`Put`, `Search`) with tenant/project/model/dimension/
  version namespaced keys, so cross-tenant candidates cannot collide or be
  returned.
- `MemorySemanticStore`: default in-memory cosine store (Lite/tests).
- Namespace helper + version invalidation.

### Cacheability Gates
- Tool calls are never semantic-cached.
- A `cache=false` or `Cache-Control: no-cache` directive bypasses lookup and write.
- Security-policy-blocked requests are never served from or written to the cache.

### Pipeline Integration
- `SemanticHandler` in the Stage 3 preflight: embed → search → threshold → on a
  candidate above threshold populate the response and return
  `SkipResolutionExecution` with `source=semantic_cache`.
- `WriteBackSemantic` stores the exact-key response so replay is deterministic;
  hits run current Output Guardrail + Accounting via the fixed pipeline.
- `semantic_cache` hits/misses are emitted through the `cache.Metrics` adapter
  (no request-unique labels).

### Console
- Cache overview in Governance shows a localized Semantic Cache note
  (tenant-isolated vectors; hits re-run current Output Guardrail and Accounting).

## Gate Results

| Gate | Result |
| --- | --- |
| `go test -race -timeout 180s ./...` (Postgres 55432 + Redis 56379) | PASS |
| `go vet ./...` | PASS |
| `go run ./cmd/architecture-test` | PASS |
| `npm run check` (format, locales, hardcoded strings, vitest, build) | PASS |
| `actionlint` | PASS |
| `gitleaks --config .gitleaks.toml detect` | no leaks found |
| `openspec validate --all --strict` | PASS |

## Tests

- Cross-tenant isolation penetration (tenant B's vector never returned to
  tenant A); threshold behavior (identical ~1.0, orthogonal ~0); namespace
  version invalidation.
- Gates: tool call not cached, no-cache bypass, security-blocked not served.
- Pipeline: semantic hit short-circuits execution but still runs Output
  Guardrail and Accounting; metrics reach the sink (hit/miss recorded).

## Scope Note

The semantic store's default backend is in-memory; Redis Vector / pgvector are
Integrate options (V8.2 §11.2) and not required for this slice. Live semantic
hit/miss counters on the Cache overview require a metrics-query surface, which
is a follow-up; the Cache overview shows the semantic capability note and
metrics are emitted to the MetricSink.
