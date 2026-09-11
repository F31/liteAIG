# Phase 4 — Groundedness + LLM-as-Judge: DoD Evidence

Status: Complete (12/12 tasks)

This change adds advanced output guardrails to the Stage 6 Output Guardrail:
a Groundedness verdict (response vs context) and a rubric-based LLM-as-Judge,
both behind replaceable contracts, wired as optional checkpoints with
block/mark/retroactive actions and redacted Security Events.

## Capabilities Delivered

### Groundedness (`groundedness-check`)
- `internal/guardrail/groundedness`: `Checker` contract (`Check(ctx, response,
  context) Verdict`) and a deterministic `OverlapChecker` (content-overlap +
  contradiction heuristics) that makes no external-model call. A model judge may
  back the same contract (Integrate option).

### LLM-as-Judge (`llm-as-judge`)
- `internal/guardrail/judge`: `Judge` contract (`Verdict(ctx, response, rubric)
  JudgeResult` with pass/fail + reason + score) and a deterministic `MockJudge`.

### Output Guardrail Integration
- The Output Guardrail handler (`internal/gateway/guardrail/handler.go`) accepts
  optional `Groundedness` and `Judge` checkpoints with a configurable `Action`
  (block / mark / retroactive). Failures emit redacted Security Events (content
  hash + reason, never raw response bodies). Unconfigured, the stage behaves
  exactly as before.

## Gate Results

| Gate | Result |
| --- | --- |
| `go test -race -timeout 180s ./...` (Postgres 55432 + Redis 56379) | PASS |
| `go vet ./...` | PASS |
| `go run ./cmd/architecture-test` | PASS |
| `actionlint` | PASS |
| `gitleaks --config .gitleaks.toml detect` | no leaks found |
| `openspec validate --all --strict` | 18 passed, 0 failed |

## Tests

- Groundedness: contradiction flagged, off-topic flagged, grounded passes, empty
  context passes, baseline makes no provider call.
- Judge: fail signal returns pass/fail + reason + score; score bounds; mock makes
  no provider call.
- Integration: block rejects before release, mark records a redacted event,
  retroactive does not block, unconfigured stage unchanged.
- Scenario D strengthening golden assertion: advanced failure records a redacted
  security event (never the raw response).

## Scope Note

The default checkpoints (OverlapChecker, MockJudge) make no external-model call,
so external-judge latency is inherently zero in the default path and does not
enter Core overhead; a model-backed judge is an Integrate option behind the
contracts. OpenAI-compatible judge grounding and advanced DLP remain separate
Integrate items.
