# exact-cache Specification (Delta)

## ADDED Requirements

### Requirement: Semantic cache shares the cache surface
The Cache overview SHALL include the Semantic Cache path (hits, misses, savings) alongside Exact Cache, and both SHALL preserve the invariant that a cache hit runs the current Output Guardrail and Accounting.

#### Scenario: Semantic path surfaced with exact
- **WHEN** an operator views the Cache overview
- **THEN** semantic hits/misses are shown alongside exact hits/misses
- **AND** both record `CacheHit` with their source (exact vs semantic_cache)
