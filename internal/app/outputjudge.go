package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/F31/liteAIG/internal/guardrail/judge"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

// judgeCallTimeout bounds the LLM-as-Judge call so a slow judge model never
// stalls the output stage; on timeout the checkpoint is skipped (fail open),
// matching the handler's treatment of judge errors.
const judgeCallTimeout = 8 * time.Second

// snapshotJudge produces an LLM verdict by calling the tenant's own provider
// (the same connector path the data plane uses for model calls). It resolves a
// deployment from the snapshot on demand.
type snapshotJudge struct {
	resolver *providerResolver
}

// boundJudge implements guardrail/judge.Judge for one request's snapshot +
// configured judge model.
type boundJudge struct {
	inner    *snapshotJudge
	snapshot *runtime.TenantRuntimeSnapshot
	model    string
}

var _ judge.Judge = (*boundJudge)(nil)

func (j *boundJudge) Verdict(ctx context.Context, response, rubric string) (judge.JudgeResult, error) {
	deployment, ok := capabilityDeployment(j.snapshot, j.model, "chat")
	if !ok {
		// No usable deployment for the judge model: skip the checkpoint.
		return judge.JudgeResult{}, fmt.Errorf("no deployment for judge model %q", j.model)
	}
	invoker, ok := j.inner.resolver.ResolveCredential(j.snapshot, deployment, deployment.CredentialID)
	if !ok {
		return judge.JudgeResult{}, fmt.Errorf("no credential for judge deployment %s", deployment.ID)
	}
	callCtx, cancel := context.WithTimeout(ctx, judgeCallTimeout)
	defer cancel()
	temperature := 0.0
	result, err := invoker.Invoke(callCtx, contracts.InvocationRequest{
		Target:       contracts.TargetRef{Kind: "model", ID: deployment.ID},
		CredentialID: deployment.CredentialID,
		Request: &interaction.UnifiedRequest{
			Kind:  interaction.RequestChat,
			Model: j.model,
			Chat: &interaction.ChatPayload{
				Messages: []interaction.Message{
					{Role: "system", Content: judgeSystemPrompt},
					{Role: "user", Content: fmt.Sprintf("Rubric:\n%s\n\nResponse:\n%s", rubric, response)},
				},
				Temperature: &temperature,
			},
		},
	})
	if err != nil {
		return judge.JudgeResult{}, err
	}
	if result == nil || result.Response == nil || len(result.Response.Choices) == 0 {
		return judge.JudgeResult{}, fmt.Errorf("judge model returned no choices")
	}
	return parseJudgeVerdict(result.Response.Choices[0].Message.Content)
}

const judgeSystemPrompt = `You are a strict output-quality judge for an AI gateway.
Evaluate the model's response against the rubric below.
Respond with ONLY a JSON object, no prose, in this exact shape:
{"passed": true or false, "reason": "short reason", "score": 0.0 to 1.0}
Set passed=false if the response violates the rubric, is unsafe, or is clearly wrong.`

// parseJudgeVerdict parses the judge model's JSON verdict. A missing/invalid
// verdict is treated as a pass (fail open) so a flaky judge model cannot block
// legitimate traffic; a well-formed failing verdict is honored.
func parseJudgeVerdict(content string) (judge.JudgeResult, error) {
	trimmed := strings.TrimSpace(content)
	if start := strings.Index(trimmed, "{"); start >= 0 {
		trimmed = trimmed[start:]
	}
	if end := strings.LastIndex(trimmed, "}"); end >= 0 {
		trimmed = trimmed[:end+1]
	}
	var parsed struct {
		Passed bool    `json:"passed"`
		Reason string  `json:"reason"`
		Score  float64 `json:"score"`
	}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return judge.JudgeResult{Passed: true, Reason: "judge verdict unparsable; fail open"}, nil
	}
	return judge.JudgeResult{Passed: parsed.Passed, Reason: parsed.Reason, Score: parsed.Score}, nil
}
