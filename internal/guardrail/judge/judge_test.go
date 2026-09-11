package judge

import (
	"context"
	"testing"
)

func TestMockJudgeFailSignal(t *testing.T) {
	judge := NewMockJudge([]string{"unsupported", "dangerous"})
	ctx := context.Background()
	result, err := judge.Verdict(ctx, "this is a dangerous recommendation", "safety rubric")
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed || result.Score > 0.5 || result.Reason == "" {
		t.Fatalf("judge = %+v", result)
	}
}

func TestMockJudgePass(t *testing.T) {
	judge := NewMockJudge([]string{"unsupported"})
	result, err := judge.Verdict(context.Background(), "a safe helpful answer", "quality rubric")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed || result.Score < 0.9 {
		t.Fatalf("judge = %+v", result)
	}
}

func TestMockJudgeScoreBounds(t *testing.T) {
	judge := NewMockJudge([]string{"x"})
	passResult, _ := judge.Verdict(context.Background(), "fine", "r")
	failResult, _ := judge.Verdict(context.Background(), "x here", "r")
	if passResult.Score < 0 || passResult.Score > 1 || failResult.Score < 0 || failResult.Score > 1 {
		t.Fatalf("scores out of bounds: %+v %+v", passResult, failResult)
	}
}
