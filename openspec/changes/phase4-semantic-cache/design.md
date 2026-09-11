# Phase 4 — Semantic Cache: Design

## Context

Phase 2 delivered the Exact Cache: a tenant/project-scoped `cache.Key`, a
replaceable `cache.Store` (Memory LRU + Redis), a Stage 3 handler returning
`SkipResolutionExecution` on hit with `WriteBack` after Accounting, and the
invariant that cache hits still run current Output Guardrail + Accounting. This
change adds the semantic path (`Prompt → Embedding → tenant-scoped vector search
→ threshold → candidate → Output Guardrail`) reusing those contracts and the
tenant-namespace pattern.

## Goals / Non-Goals

**Goals:**
- A `SemanticStore` contract with embedding + tenant-scoped vector search +
  similarity threshold.
- Tenant/project namespace isolation in the vector index (embedding model +
  dimension + cache version in the namespace), with a cross-tenant penetration
  test.
- Cacheability gates: tool calls never cached; security-policy-blocked requests
  never served from cache; `no-cache` bypasses lookup and write.
- Cache hits still run current Output Guardrail + Accounting; `source=semantic_cache`.
- Semantic cache overview + metrics through the existing MetricSink.

**Non-Goals:**
- OIDC/SAML/SCIM, ClickHouse sink, Multi-AZ/DR, Groundedness/LLM-as-Judge,
  gRPC Extension Bridge, A2A advanced.

## Decisions

### 1. Semantic cache is a tenant-namespaced vector index behind a contract
New `internal/cache/semantic.go` defines `SemanticStore`:
`Put(tenant, project, key, vector)`, `Search(tenant, project, vector, limit)
([]Candidate)`. The in-memory cosine store is the default (Lite/test); a Redis
Vector backend is an Integrate option. Every vector key is namespaced by
`tenant:{id}:project:{id}:model:{embeddingModel}:dim:{dim}:ver:{cacheVersion}` so
cross-tenant candidates cannot collide or be returned.
Alternative considered: a global index. Rejected by V8.2 §11.2 tenant isolation.

### 2. Cacheability gates mirror the Exact Cache with a security gate
A request is cacheable only when: policy enabled, kind allowed, temperature
within bounds, no tool call, no `no-cache` directive, and the request is not
blocked by a security policy. A blocked request is never written to or served
from the semantic cache.
Alternative considered: caching post-guardrail responses. Rejected — must run
current Output Guardrail on every hit.

### 3. Semantic hits reuse the pipeline short circuit
A semantic handler (part of the Stage 3 preflight) embeds the normalized input,
searches the tenant namespace, applies the threshold, and on a candidate above
threshold populates `request.Response` and returns `SkipResolutionExecution`.
`WriteBack` stores the response keyed by the exact normalized request (not the
vector) so replay is deterministic; the vector index is updated in the same
path. Cache hits run Output Guardrail + Accounting via the runner.
Alternative considered: returning cached content without the pipeline. Rejected
by the invariant.

### 4. Metrics and overview reuse existing wiring
`CacheMetricsAdapter` already forwards `cache.hits/misses/savings_tokens` to the
MetricSink; the semantic path emits `semantic_cache.hits/misses` through the
same adapter, and the Cache overview shows semantic hits/misses alongside exact.

## Risks / Trade-offs

- [Vector index grows] → bounded in-memory store + Redis Vector option; namespace version invalidation.
- [Similarity false positives] → configurable threshold + tenant namespace; cacheability gates.
- [Embedding cost on miss] → embed only when cacheable and policy-enabled; semantic lookup is off the TTFT hot path for exact-key hits.
- [Security policy changes] → hits still run current Output Guardrail; blocked policy never writes.

## Migration Plan

1. Add `SemanticStore` contract + in-memory cosine store + namespace keys.
2. Add embedding abstraction (a function provider; no external dependency by default).
3. Add the semantic cache handler + `WriteBack` integration; reuse `SkipResolutionExecution`.
4. Add cacheability gates (tool-call, no-cache, security-blocked) with tests.
5. Add metrics + Cache overview surface; cross-tenant penetration test.
6. Run full gate set + OpenSpec strict validation + record DoD evidence.

Rollback: semantic cache disabled by default; exact cache path unchanged.
