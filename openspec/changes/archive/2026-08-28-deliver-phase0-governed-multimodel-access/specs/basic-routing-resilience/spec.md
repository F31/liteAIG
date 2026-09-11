## ADDED Requirements

### Requirement: Hard-constrained candidate set
Route planning SHALL exclude disabled, unhealthy, circuit-open, capability-incompatible, context-incompatible, policy-denied, residency-incompatible, or cross-tenant deployments before applying any selection algorithm. Fallback candidates SHALL come only from the resulting eligible set.

#### Scenario: Ineligible low-cost deployment
- **WHEN** a deployment would otherwise be preferred but violates a hard constraint
- **THEN** it is excluded from both primary and fallback selection
- **AND** decision evidence records each exclusion reason

### Requirement: Deterministic Phase 0 selection
Phase 0 SHALL support Priority, Weighted, and RoundRobin policies. Priority ties SHALL resolve by stable deployment ID; Weighted selection SHALL be reproducible from request or session identity and configured weights; RoundRobin SHALL use a concurrency-safe snapshot-local sequence with stable deployment ordering.

#### Scenario: Reproducible weighted decision
- **WHEN** the same eligible set, weights, snapshot version, and routing key are evaluated
- **THEN** Weighted selection returns the same deployment and candidate evidence

#### Scenario: Concurrent round robin
- **WHEN** concurrent requests use RoundRobin over healthy eligible deployments
- **THEN** selection advances without a data race
- **AND** no ineligible deployment is selected

### Requirement: Bounded timeout and retry
Execution SHALL enforce configured connect, first-byte, total, and stream-idle timeouts. Network retries SHALL default to at most two attempts, use bounded exponential backoff with jitter, and count toward a maximum total upstream-call budget.

#### Scenario: Retryable timeout
- **WHEN** an upstream attempt times out before client-visible stream output and retry budget remains
- **THEN** the system retries according to policy
- **AND** records the failed attempt and backoff in request evidence

#### Scenario: Client-visible stream output
- **WHEN** any stream chunk has been committed to the client
- **THEN** the gateway does not perform a full automatic retry or fallback that could duplicate output

### Requirement: Basic circuit exclusion
Phase 0 SHALL maintain an in-process circuit state per provider, deployment, and credential. After at least 20 measured attempts, an error rate above 50 percent SHALL open the circuit for an initial 30-second cooldown; after cooldown, one guarded probe SHALL close the circuit on success or reopen it on failure.

#### Scenario: Circuit opens
- **WHEN** the minimum sample size is reached and retryable error rate exceeds the threshold
- **THEN** subsequent route plans exclude that provider/deployment/credential tuple during cooldown
- **AND** the transition is recorded as an operational event

### Requirement: Policy-governed fallback
Connect errors, eligible timeouts, 429, 5xx, circuit transitions, and credential exhaustion MAY trigger the next planned deployment within total call and time budgets. Business 4xx responses MUST NOT trigger fallback by default.

#### Scenario: Provider 429 fallback
- **WHEN** the selected deployment returns 429 and fallback budget remains
- **THEN** execution invokes the next eligible planned deployment
- **AND** Request Explorer evidence identifies the trigger and added attempt

#### Scenario: Business 400 response
- **WHEN** an upstream returns a normalized non-retryable business 400 response
- **THEN** execution returns the normalized error without invoking a fallback deployment

### Requirement: Explainable route evidence
Every route decision SHALL record snapshot and policy versions, considered deployments, exclusion reasons, algorithm inputs, selected deployment, fallback order, attempt outcomes, and final result without exposing secrets.

#### Scenario: Failed request explanation
- **WHEN** all planned deployments fail
- **THEN** Request Explorer can reconstruct why each candidate was selected, skipped, retried, or exhausted
