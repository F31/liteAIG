package groundedness

import (
	"context"
	"testing"
)

func TestOverlapCheckerFlagsContradiction(t *testing.T) {
	checker := NewOverlapChecker(0.5)
	ctx := context.Background()
	verdict, err := checker.Check(ctx, "on the contrary, the sky is green", "the sky is blue and clear today")
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Passed {
		t.Fatalf("contradiction not flagged: %+v", verdict)
	}
}

func TestOverlapCheckerFlagsOffTopic(t *testing.T) {
	checker := NewOverlapChecker(0.2)
	ctx := context.Background()
	verdict, err := checker.Check(ctx, "the stock market closed higher on tuesday", "how to refund a purchase within thirty days")
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Passed {
		t.Fatalf("off-topic response not flagged: %+v", verdict)
	}
}

func TestOverlapCheckerPassesGrounded(t *testing.T) {
	checker := NewOverlapChecker(0.2)
	ctx := context.Background()
	verdict, err := checker.Check(ctx, "you can refund a purchase within thirty days", "refund within thirty days for eligible purchases")
	if err != nil {
		t.Fatal(err)
	}
	if !verdict.Passed {
		t.Fatalf("grounded response flagged: %+v", verdict)
	}
}

func TestOverlapCheckerEmptyContextPasses(t *testing.T) {
	checker := NewOverlapChecker(0)
	verdict, err := checker.Check(context.Background(), "anything", "")
	if err != nil || !verdict.Passed {
		t.Fatalf("empty context = %+v, %v", verdict, err)
	}
}
