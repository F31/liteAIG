## 1. Freeze Phase 0 Contracts

- [x] 1.1 Freeze the canonical Go module path as `github.com/F31/liteAIG`.
- [x] 1.2 Resolve and record the protocol compatibility matrix, budget unit, indicative pricing policy, and blocking benchmark environment from design Open Questions.
- [x] 1.3 Initialize the Go module and minimal executable composition root; add domain and platform paths only when their first behavior is implemented.
- [x] 1.4 Define Kernel stage, request/interaction context, runtime, connector, event, clock, ID, and stable error contracts with contract-level tests.
- [x] 1.5 Add module, forbidden-import, and table-owner manifests plus an `architecture-test` command that reports actionable violations.
- [x] 1.6 Add CI gates for formatting, static analysis, unit tests, architecture tests, OpenSpec validation, and secret scanning.

## 2. Establish Tenant-owned Persistence

- [x] 2.1 Add owner-annotated migrations for tenants, projects, API keys, pepper versions, providers, credentials, deployments, logical models, route policies, config drafts/versions, request records, usage facts, budget state, and audit events.
- [x] 2.2 Implement TenantScope-required Tenant/Project repository contracts and the SQLite adapter; add other owner repositories with their first owning behavior.
- [x] 2.3 Implement the PostgreSQL Tenant/Project adapter and CI conformance fixture required by the resolved Phase 0 policy.
- [x] 2.4 Add shared Tenant/Project repository conformance tests covering transactions, migration idempotency, pagination, scope enforcement, and cross-tenant negative cases.
- [x] 2.5 Implement local-admin bootstrap with Argon2id and atomic creation of the first Tenant and Default Project.

## 3. Build Versioned Runtime Configuration

- [x] 3.1 Implement server-side Draft and ChangeSet persistence with active-version optimistic concurrency.
- [x] 3.2 Implement schema, reference, tenant-scope, route-viability, policy, and secret-reference validation with blocking errors and acknowledgeable warnings.
- [x] 3.3 Implement semantic secret-safe diff and automated tests proving plaintext secrets never enter diff, logs, or audit payloads.
- [x] 3.4 Implement the config compiler that produces immutable GlobalRuntime and TenantRuntimeSnapshot indexes.
- [x] 3.5 Implement atomic per-tenant snapshot activation and request-lifetime snapshot pinning with race tests.
- [x] 3.6 Implement versioned publication and rollback audit events, including failure behavior that preserves the previous active snapshot.
- [x] 3.7 Add an integration test proving a loaded Data Plane continues serving when Control Plane repositories are unavailable.

## 4. Implement Tenant-scoped Authentication

- [x] 4.1 Implement CSPRNG `tenant_ref`, key public ID, and Virtual Key secret generation with one-time return behavior.
- [x] 4.2 Implement versioned-pepper HMAC storage, constant-time verification, fingerprints, status, expiry, and network checks.
- [x] 4.3 Compile API keys and Principal bindings into tenant runtime indexes and authenticate without hot-path repository access.
- [x] 4.4 Add non-enumerating invalid-key responses and tests for malformed tokens, unknown/wrong secrets, revoked/expired keys, suspended tenants, and scope override attempts.
- [x] 4.5 Add cross-tenant security tests for Key and compiled Provider/Credential/Deployment/Logical Model resources; Request/Usage repository isolation remains part of owning task 7.8.

## 5. Implement Protocol and Connector Compatibility

- [x] 5.1 Define canonical model request, response, stream, usage, capability, parameter-support, and normalized upstream-error types.
- [x] 5.2 Implement OpenAI Chat Completions, Embeddings, and Models ingress normalization with supported non-streaming and streaming fixtures.
- [x] 5.3 Implement Anthropic Messages normalization with supported non-streaming and streaming fixtures.
- [x] 5.4 Implement OpenAI, OpenAI-Compatible, and Anthropic connectors with cancellation, health, timeout, and error normalization.
- [x] 5.5 Implement strict/permissive supported-parameter handling and explicit provider-passthrough allowlists.
- [x] 5.6 Build connector contract suites for normal, timeout, 429, 5xx, stream-drop, client cancellation, usage normalization, and secret-redaction cases.
- [x] 5.7 Implement tenant/key-filtered `/v1/models` output that exposes Logical Models only.

## 6. Implement Routing and Resilience

- [x] 6.1 Implement hard-constraint filtering with structured exclusion evidence for status, scope, ACL, capability, context, residency, health, circuit, and credential availability.
- [x] 6.2 Implement deterministic Priority and Weighted route selection with stable tie-breaking and reproducibility tests.
- [x] 6.3 Implement concurrency-safe snapshot-local RoundRobin selection and race tests.
- [x] 6.4 Build immutable RoutePlan output containing policy/snapshot versions, candidate evidence, selected deployment, and fallback order.
- [x] 6.5 Implement connect, first-byte, total, and stream-idle timeout handling plus bounded retry/backoff and total-call limits.
- [x] 6.6 Implement the process-local Phase 0 circuit and guarded cooldown probe with transition events and deterministic-clock tests.
- [x] 6.7 Implement fallback triggers, business-4xx exclusion, credential exhaustion, and no-replay-after-stream-commit behavior.
- [x] 6.8 Add failure-injection tests for timeout, 429, 5xx, slow first token, open circuit, exhausted credential, and dropped stream.

## 7. Assemble Governance and Accounting Pipeline

- [x] 7.1 Implement the compile-time seven-stage runner and prove stage ordering and non-extensibility in tests.
- [x] 7.2 Implement Admission body-size checks, snapshot authentication, scope resolution, request IDs, and basic model ACL enforcement.
- [x] 7.3 Implement deterministic keyword, regex, and secret Fast Guard rules for input and non-stream output with redacted security events.
- [x] 7.4 Implement process-local tenant/project simple RPM enforcement with deterministic clock tests.
- [x] 7.5 Implement single-window hard/soft budget reservation and idempotent reconciliation behind a replaceable budget contract.
- [x] 7.6 Integrate Resolution and Execution through connector contracts without protocol or domain boundary violations.
- [x] 7.7 Implement the top-level idempotent finalizer for success, denial, block, failure, timeout, cancellation, and race conditions.
- [x] 7.8 Persist request, route, attempt, token, indicative-cost availability, budget, guardrail, and audit facts without prompt/response retention.
- [x] 7.9 Emit redacted standard domain events and verify sink failure cannot change the client outcome or duplicate durable facts.

## 8. Deliver Control APIs and Console Journey

- [x] 8.1 Implement scoped Admin API handlers and typed backend ports for setup, projects, runtime resources, keys, drafts, versions, publication, rollback, requests, usage, Playground, and audit.
- [x] 8.2 Initialize the React/TypeScript/Vite Console with Ant Design tokens, route-level splitting, typed API boundary, and Go `embed.FS` packaging.
- [x] 8.3 Implement secure local-admin session cookies, backend authorization, transient secret handling, and browser security headers.
- [x] 8.4 Implement the Setup Wizard including provider test, model selection, default resources, Virtual Key one-time view, first Playground call, and SDK example.
- [x] 8.5 Implement basic Projects, Models, Keys, Routing, Dashboard, Audit, and Config Draft/Diff surfaces needed by the wizard and recovery paths.
- [x] 8.6 Implement Playground through a short-lived test Principal and the production pipeline with `source=playground` accounting context.
- [x] 8.7 Implement basic Request Explorer history/detail with route, attempt, guardrail, usage, cost-availability, latency, and configuration evidence.
- [x] 8.8 Implement request-ID navigation and consistency tests between Playground and Request Explorer.
- [x] 8.9 Implement tenant-switch teardown for streams, queries, cache, project selection, and temporary Draft UI state.
- [x] 8.10 Add responsive behavior, non-color status semantics, keyboard navigation, axe tests, and the Lighthouse Accessibility 90 release gate.

## 9. Prove Phase 0 Release Readiness

- [x] 9.1 Add reproducible per-stage and end-to-end benchmarks using the approved environment and blocking overhead threshold.
- [x] 9.2 Add API/SDK compatibility fixtures for the frozen OpenAI and Anthropic matrix, including streaming and normalized errors.
- [x] 9.3 Add Golden Scenario A with OpenAI-compatible and Anthropic protocol families, multiple deployments, provider switching, and no Provider Bridge dependency.
- [x] 9.4 Assert Scenario A completes the guided first call within five minutes and correlates Tenant, Project, Key, Logical Model, deployment, token, cost availability, latency, and request ID.
- [x] 9.5 Run architecture, unit, race, repository, connector-contract, integration, security, benchmark, UI, accessibility, and Golden Scenario suites and resolve all blocking failures.
- [x] 9.6 Validate this OpenSpec change in strict mode and record Phase 0 DoD evidence before marking any Phase 1 production task eligible.
