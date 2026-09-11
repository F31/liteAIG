## MODIFIED Requirements

### Requirement: Basic circuit exclusion
Phase 0 SHALL maintain an in-process circuit state per provider, deployment, and credential. After at least 20 measured attempts, an error rate above 50 percent SHALL open the circuit for an initial 30-second cooldown; after cooldown, one guarded probe SHALL close the circuit on success or reopen it on failure. Phase 1 SHALL replace this process-local behavior with the complete circuit state machine specified by `full-circuit-state-machine` while preserving the compiled defaults and the same per-tuple granularity.

#### Scenario: Circuit opens
- **WHEN** the minimum sample size is reached and retryable error rate exceeds the threshold
- **THEN** subsequent route plans exclude that provider/deployment/credential tuple during cooldown
- **AND** the transition is recorded as an operational event

#### Scenario: Phase 1 cooldown escalation
- **WHEN** the circuit remains open across repeated failures under the full state machine
- **THEN** cooldown escalates according to the configured schedule
- **AND** a single guarded half-open probe is admitted per window

### Requirement: Policy-governed fallback
Connect errors, eligible timeouts, 429, 5xx, circuit transitions, and credential exhaustion MAY trigger the next planned deployment within total call and time budgets. Business 4xx responses MUST NOT trigger fallback by default. Phase 1 SHALL additionally rotate to the next eligible credential in the configured pool on retryable 429 or credential exhaustion before moving to the next deployment.

#### Scenario: Provider 429 fallback
- **WHEN** the selected deployment returns 429 and fallback budget remains
- **THEN** execution invokes the next eligible planned deployment
- **AND** Request Explorer evidence identifies the trigger and added attempt

#### Scenario: 429 credential rotation before deployment fallback
- **WHEN** the active credential returns a retryable 429 and an eligible credential remains in the pool
- **THEN** execution rotates to the next eligible credential
- **AND** records each attempt with its credential identity

#### Scenario: Business 400 response
- **WHEN** an upstream returns a normalized non-retryable business 400 response
- **THEN** execution returns the normalized error without invoking a fallback deployment
