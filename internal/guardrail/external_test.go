package guardrail

import (
	"context"
	"errors"
	"testing"
)

func TestExternalGuardrailFailModes(t *testing.T) {
	provider := externalProvider{err: errors.New("down")}
	observer := &externalObserver{}
	result, err := EvaluateExternal(context.Background(), provider, ExternalRequest{}, ExternalFailOpen, observer)
	if err != nil || !result.Degraded || observer.calls != 1 {
		t.Fatalf("fail-open = %+v, %v", result, err)
	}
	if _, err := EvaluateExternal(context.Background(), provider, ExternalRequest{}, ExternalFailClosed, observer); !errors.Is(err, ErrExternalUnavailable) {
		t.Fatalf("fail-closed error = %v", err)
	}
	result, err = EvaluateExternal(context.Background(), provider, ExternalRequest{}, ExternalBypass, observer)
	if err != nil || !result.Degraded || result.Reason != "bypassed" {
		t.Fatalf("bypass = %+v, %v", result, err)
	}
}

type externalProvider struct{ err error }

func (p externalProvider) Evaluate(context.Context, ExternalRequest) (ExternalResult, error) {
	return ExternalResult{}, p.err
}

type externalObserver struct{ calls int }

func (o *externalObserver) RecordExternalFailure(ExternalFailMode) { o.calls++ }
