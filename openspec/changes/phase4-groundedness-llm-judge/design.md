# Phase 4 — Groundedness + LLM-as-Judge: Design

## Context

The Output Guardrail stage (`internal/gateway/guardrail/handler.go`) runs a
deterministic `builtin.Engine` on response choices and tool results, emits
content-hash Security Events, and the observability `LatencyBreakdown` already
separates Core / provider-network / external-guardrail time. This change adds two
optional, replaceable checkpoints to that stage: Groundedness (response vs
provided context) and LLM-as-Judge (rubric verdict). Both run after the
deterministic rules and before the response is released; a failing verdict blocks
or marks per the configured action.

## Goals / Non-Goals

**Goals:**
- `Groundedness` contract with a deterministic baseline (content-overlap
  signals) and a replaceable model judge.
- `Judge` contract (LLM-as-Judge): pass/fail + reason + score over a rubric.
- Both wired into the Output Guardrail stage as configurable checkpoints with
  block / mark / retroactive actions.
- Redacted Security Events (no raw response bodies) and external-judge latency
  split from Core overhead.

**Non-Goals:**
- OIDC/SAML/SCIM, ClickHouse sink, Multi-AZ/DR, gRPC Extension Bridge,
  Phase 5 graph analytics.

## Decisions

### 1. Groundedness is a contract with a deterministic baseline
`internal/guardrail/groundedness`: `Checker` with `Check(ctx, response, context)
Verdict`. The default `OverlapChecker` uses content-overlap and
contradiction heuristics (lexical overlap with the context, negation/contrast
signals) and needs no external model. A model-based `JudgeGrounder` can back the
same contract.
Alternative considered: LLM-only. Rejected — a deterministic baseline keeps the
default off the external-model hot path and is testable without a provider.

### 2. LLM-as-Judge is a replaceable contract
`internal/guardrail/judge`: `Judge` with `Verdict(ctx, response, rubric)
(JudgeResult)`. `JudgeResult` carries pass/fail, a reason, and a score. The
default `MockJudge` is deterministic for tests; an OpenAI-compatible provider
implements the same contract (Integrate, not required).
Alternative considered: folding judge into the deterministic engine. Rejected —
the rubric-based verdict is a different capability.

### 3. Checkpoints run after deterministic rules, before release
The Output Guardrail handler gains optional `Groundedness` and `Judge` fields.
When configured, the response is evaluated after deterministic rules; a failing
verdict applies the configured action (`block` rejects the response,
`mark` tags it, `retroactive` records without blocking). A redacted Security
Event is emitted on failure.

### 4. External-judge latency is separated
The judge/groundedness model call time is recorded into the existing
`LatencyBreakdown.ExternalGuardrailMS`, not Core, so TTFT/Core overhead stays
accurate. Non-model baseline checks cost ~0 and do not add latency.

## Risks / Trade-offs

- [Judge quality variability] → replaceable contract; mock default; evidence window on verdict.
- [Latency on output path] → external judge is off TTFT; baseline is ~0; latency split.
- [False blocks] → configurable action (block/mark/retroactive) and redacted evidence for review.

## Migration Plan

1. Add `internal/guardrail/groundedness` (contract + OverlapChecker) with tests.
2. Add `internal/guardrail/judge` (contract + MockJudge) with tests.
3. Wire both into the Output Guardrail handler with configurable action; redacted Security Events; latency split.
4. Add a Scenario D strengthening assertion and run the full gate set; validate OpenSpec strict; record DoD evidence.

Rollback: checkpoints are optional; without a configured Groundedness/Judge the
handler behaves exactly as before.
