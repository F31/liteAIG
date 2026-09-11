## 1. Groundedness

- [x] 1.1 Add `internal/guardrail/groundedness`: `Checker` contract (`Check(ctx, response, context) Verdict`) and a default `OverlapChecker` (content-overlap/contradiction heuristics, no external model).
- [x] 1.2 Add a `JudgeGrounder` adapter so a model judge can back the same contract (Integrate option).
- [x] 1.3 Add tests: contradiction flagged, off-topic flagged, baseline makes no provider call, verdict carries evidence.

## 2. LLM-as-Judge

- [x] 2.1 Add `internal/guardrail/judge`: `Judge` contract (`Verdict(ctx, response, rubric) JudgeResult` with pass/fail + reason + score) and a deterministic `MockJudge`.
- [x] 2.2 Add a note/interface for an OpenAI-compatible provider behind the same contract (no new dependency by default).
- [x] 2.3 Add tests: mock verdict pass/fail with reason + score, score bounds, no provider call.

## 3. Output Guardrail Integration

- [x] 3.1 Wire optional `Groundedness` and `Judge` into the Output Guardrail handler with a configurable action (block / mark / retroactive).
- [x] 3.2 Emit redacted Security Events on failures (content hashes + reason, no raw bodies).
- [x] 3.3 Record external judge/groundedness latency into the existing `LatencyBreakdown.ExternalGuardrailMS` (not Core).
- [x] 3.4 Add tests: block rejects before release, mark tags, retroactive records, unconfigured stage unchanged, latency split, redacted event.

## 4. Console and Release Gates

- [x] 4.1 Surface groundedness/judge failures in the Security Events surface (localized).
- [x] 4.2 Add a Scenario D strengthening assertion (advanced output guardrail; redacted evidence).
- [x] 4.3 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures.
- [x] 4.4 Run `openspec validate --all --strict` and record Phase 4 groundedness/judge DoD evidence.
