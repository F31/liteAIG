# Phase 4 — Semantic Cache

## Why

V8.2 Phase 4 lists Semantic Cache as the first backend item: `Prompt → Embedding → tenant-scoped vector search → threshold → candidate → current Output Guardrail → return`. It is the P0-Commercial Exact Cache's natural companion for high-latency, semantically-repeated prompts. It reuses the Exact Cache contracts, the tenant-namespace isolation pattern, and the "cache hit still runs Output Guardrail" invariant already proven in Phase 2, and its tenant-isolation penetration test is an explicit V8.2 DoD item.

## What Changes

- Add a Semantic Cache store behind a replaceable `SemanticStore` contract: embedding an input, tenant-scoped vector search over candidates, a similarity threshold, and returning the best candidate. Default backend is in-memory cosine over compiled vectors; a Redis Vector backend is an Integrate option (V8.2 §11.2).
- Enforce tenant/project namespace isolation in the vector store (embedding model + dimension + cache version written into the namespace), so a cross-tenant candidate can never be returned.
- Gate cacheability: tool calls are never semantic-cached; a request blocked or restricted by a security policy must not be served from cache; an explicit `cache=false` or `Cache-Control: no-cache` bypasses lookup and write.
- Cache hits still run the current Output Guardrail and Accounting on the cached response (same invariant as Exact Cache), and the request records `CacheHit`/`source=semantic_cache`.
- Expose the semantic cache on the Cache overview surface and add metrics (hit/miss/eviction) through the existing MetricSink.

## Capabilities

### New Capabilities
- `semantic-cache`: tenant-scoped vector semantic cache with replaceable store, threshold scoring, isolation, and cacheability gates.

### Modified Capabilities
- `exact-cache`: the Cache surface and metrics now include the semantic cache path; cache hits continue to run Output Guardrail and Accounting.

## Impact

- **Backend**: new `internal/cache/semantic.go` (embedding + vector search + threshold), a `SemanticStore` contract, an in-memory cosine store, and tenant-namespaced keys; a semantic cache handler wired alongside the Exact Cache Stage 3 path with `SkipResolutionExecution`.
- **APIs/Console**: semantic cache overview (hits/misses/savings) on the Cache surface, localized.
- **Dependencies**: no new third-party runtime dependency (Redis Vector / pgvector are Integrate options, not required).
- **Tests**: cross-tenant isolation (candidate from tenant B never returned to tenant A), threshold behavior, tool-call and no-cache bypass, cache-hit-still-guards, and metric wiring.

### Non-Goals
- OIDC/SAML/SCIM, ClickHouse sink, Multi-AZ/DR, Groundedness/LLM-as-Judge, gRPC Extension Bridge, A2A advanced (separate Phase 4 remainder items).

**Golden Scenario:** strengthens Scenario D (security policy gates cache reads) and Scenario B cost optimization (semantic repeat reduction).
