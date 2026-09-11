# commercial-guardrail Specification

## ADDED Requirements

### Requirement: Basic PII and prompt injection detection
The Guardrail Engine SHALL detect basic PII and basic Prompt Injection / Jailbreak patterns in addition to the existing deterministic keyword/regex/secret Fast Guard. Detector patterns SHALL be configurable typed policy and compiled into the Tenant Runtime Snapshot, and SHALL support block and redact actions.

#### Scenario: PII redacted before upstream
- **WHEN** an input message contains a configured PII pattern
- **THEN** the matched value is redacted or the request is blocked according to policy
- **AND** the decision is recorded as a Security Event with only a content hash

#### Scenario: Prompt injection blocked
- **WHEN** an input matches a configured Prompt Injection pattern
- **THEN** the request is blocked with a Guardrail outcome
- **AND** no upstream provider is invoked

### Requirement: Guardrail lifecycle with fast publish
Guardrail policy SHALL follow a lifecycle of edit, test case, impact preview, diff/validate, publish with Security Epoch, runtime enforcement, and Security Event. A Fast Publish SHALL update guardrail policy and its Security Epoch without requiring full configuration publication, and SHALL distinguish Tighten from Loosen semantics.

#### Scenario: Tighten publishes quickly
- **WHEN** an operator tightens a guardrail policy via Fast Publish
- **THEN** the tightened rules become active with a new Security Epoch
- **AND** the change is recorded in Audit with the prior and new epoch

#### Scenario: Impact preview uses only explicit samples
- **WHEN** an operator previews policy impact
- **THEN** only explicitly available samples are used
- **AND** no un-persisted request content is claimed as historical coverage

### Requirement: Guardrail test cases and playground
The system SHALL support versioned Guardrail Test Cases that can be replayed against a policy version, and a Guardrail-only Playground that exercises a policy without invoking providers or mutating production config.

#### Scenario: Test case replay
- **WHEN** a test case is run against a policy version
- **THEN** the expected action and outcome match
- **AND** the result is attributable to the policy and rule

### Requirement: Streaming guardrail layers
Streaming responses SHALL be governed by a three-layer model: Layer 1 inline fast guard per chunk with a cross-chunk rolling window and a small per-chunk latency budget; Layer 2 buffered local guard released at sentence boundary, newline, or a token window with a bounded window latency budget; Layer 3 async shadow guard that MUST NOT enter the TTFT path. Remote External Guardrail SHALL NOT be a synchronous per-chunk call by default.

#### Scenario: Layer 1 blocks in stream
- **WHEN** a stream chunk matches a Layer 1 rule
- **THEN** the chunk is masked or the stream is blocked
- **AND** the per-chunk latency budget is respected

#### Scenario: Cross-chunk detection
- **WHEN** a forbidden phrase is split across consecutive chunks
- **THEN** the Layer 1 rolling window detects it across chunks
- **AND** the stream is blocked or retroactively recorded

### Requirement: External Guardrail API with explicit fail modes
A replaceable External Guardrail API SHALL support explicit fail modes (fail-open, fail-closed, bypass) and SHALL expose timeouts and failures as observable metrics. An External Guardrail failure SHALL follow the configured mode and MUST NOT silently degrade the default security posture.

#### Scenario: External guardrail timeout is observable
- **WHEN** an External Guardrail call times out
- **THEN** the configured fail mode applies
- **AND** the timeout is recorded as an observable event with no secret material

### Requirement: Security events traceability
Every Guardrail decision SHALL produce a Security Event attributable to the policy, rule, request, and snapshot version. Security Events SHALL carry only redacted content hashes, never raw prompt or response bodies, and SHALL be tenant-scoped.

#### Scenario: Security event is traceable
- **WHEN** a Guardrail blocks a request
- **THEN** a Security Event records the policy, rule, request ID, and snapshot version
- **AND** the event contains no prompt or response body
