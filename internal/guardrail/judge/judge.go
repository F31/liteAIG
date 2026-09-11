// Package judge provides a replaceable model Judge (LLM-as-Judge) contract and
// a deterministic mock for tests. An OpenAI-compatible provider may implement
// the same contract; no provider is required by default.
package judge

import (
	"context"
	"strings"
)

// JudgeResult is the rubric verdict.
type JudgeResult struct {
	Passed bool
	Reason string
	Score  float64 // 0..1
}

// Judge evaluates a response against a rubric.
type Judge interface {
	Verdict(context.Context, string, string) (JudgeResult, error)
}

// MockJudge is a deterministic judge for tests and default use.
type MockJudge struct {
	FailOn []string // substrings that cause a fail verdict
}

// NewMockJudge returns a deterministic judge.
func NewMockJudge(failOn []string) *MockJudge {
	return &MockJudge{FailOn: failOn}
}

// Verdict returns pass unless the response contains a fail signal.
func (m *MockJudge) Verdict(_ context.Context, response, _ string) (JudgeResult, error) {
	lower := strings.ToLower(response)
	for _, signal := range m.FailOn {
		if strings.Contains(lower, strings.ToLower(signal)) {
			return JudgeResult{Passed: false, Reason: "failed rubric: " + signal, Score: 0.1}, nil
		}
	}
	return JudgeResult{Passed: true, Reason: "passed rubric", Score: 0.95}, nil
}
