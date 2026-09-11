# Phase 4 — Groundedness + LLM-as-Judge

## Why

V8.2 Phase 4 lists Groundedness and LLM-as-Judge as advanced guardrails: detect when a model's output is not grounded in the provided context, and use a model judge for quality/safety verdicts that a deterministic engine cannot express. Today the Output Guardrail pipeline (`internal/gateway/guardrail`, Stage 6) only runs deterministic rules (keyword/regex/PII). This change adds the groundedness and judge checkpoints behind replaceable contracts, with a mock judge for tests and an OpenAI-compatible provider as the Integrate option.

## What Changes

- Add a `Groundedness` contract: given the response and the provided context (messages/document provenance), return a verdict and evidence. A deterministic baseline implementation uses content overlap signals (no external model required); an LLM judge is the Integrate option.
- Add a `Judge` contract (LLM-as-Judge): given a response and a rubric, return a verdict (pass/fail) with a reason and score, used for quality/safety checks that deterministic rules cannot express.
- Wire both into the Output/Stream Guardrail stage as configurable checkpoints: a failing Groundedness or Judge verdict blocks or marks the response per the configured action (block / mark / retroactive).
- Emit Guardrail Security Events for groundedness/judge failures with redacted evidence (no raw response bodies), and expose them in the Security Events surface.
- Expose per-checkpoint latency separately (external-judge latency must not fold into Core overhead).

## Capabilities

### New Capabilities
- `groundedness-check`: response-vs-context groundedness verdict with a deterministic baseline and a replaceable model judge.
- `llm-as-judge`: model-judge verdict (pass/fail + reason + score) over a rubric for output quality/safety.

### Modified Capabilities
- `commercial-guardrail`: the Output Guardrail stage gains optional Groundedness and Judge checkpoints with action and retroactive semantics.

## Impact

- **Backend**: new `internal/guardrail/groundedness` and `internal/guardrail/judge` contracts + implementations; a `gateway/guardrail` checkpoint wiring; redacted Security Events; latency split in the existing `LatencyBreakdown` (external-judge time separate from Core).
- **Dependencies**: no new third-party runtime dependency (mock judge default; OpenAI-compatible provider is an Integrate option behind the contract).
- **Tests**: groundedness baseline detects contradiction/off-topic, judge verdict + reason + score, block vs mark action, redacted security event, latency split, tenant isolation.

### Non-Goals
- OIDC/SAML/SCIM, ClickHouse sink, Multi-AZ/DR orchestration, gRPC Extension Bridge, Phase 5 graph analytics (separate remainder items).

**Golden Scenario:** strengthens Scenario D (advanced output guardrail with groundedness/judge) — no raw prompt/response bodies in events.
