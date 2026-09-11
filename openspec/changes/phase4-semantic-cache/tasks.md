## 1. Semantic Store

- [x] 1.1 Add `internal/cache/semantic.go`: `SemanticStore` contract (`Put`, `Search`) with tenant/project/model/dim/version namespaced keys.
- [x] 1.2 Add an in-memory cosine store (default) and a namespace helper; prove cross-tenant candidates are never returned.
- [x] 1.3 Add an embedding provider abstraction (function-based; no external dependency by default) and a normalized-input embedding path.
- [x] 1.4 Add tests: cross-tenant isolation penetration, threshold behavior, namespace version invalidation.

## 2. Cacheability Gates

- [x] 2.1 Gate semantic cache on policy enabled, kind allowed, temperature bounds, no tool call, and no `cache=false`/`Cache-Control: no-cache`.
- [x] 2.2 Ensure security-policy-blocked requests are never served from or written to the semantic cache.
- [x] 2.3 Add tests: tool call not cached, no-cache bypass, security-blocked not served.

## 3. Pipeline Integration and Metrics

- [x] 3.1 Add a semantic cache handler in the Stage 3 preflight: embed → search → threshold → on hit populate response + `SkipResolutionExecution` (record `source=semantic_cache`).
- [x] 3.2 Wire `WriteBack` (exact-key response storage + vector index update) so replay is deterministic and hits run Output Guardrail + Accounting.
- [x] 3.3 Emit `semantic_cache.hits/misses` through the existing `CacheMetricsAdapter`/MetricSink with no request-unique labels.
- [x] 3.4 Add tests: semantic hit still runs output guardrail, cache-hit accounting, metrics reach the sink, tool-call/security gates hold.

## 4. Console and Release Gates

- [x] 4.1 Add semantic hits/misses to the Cache overview surface (localized, zh-CN/en-US parity).
- [x] 4.2 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures.
- [x] 4.3 Run `openspec validate --all --strict` and record Phase 4 semantic cache DoD evidence.
