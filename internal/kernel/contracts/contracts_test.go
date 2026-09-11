package contracts

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeInvoker struct{}

func (fakeInvoker) Invoke(context.Context, InvocationRequest) (*InvocationResponse, error) {
	return &InvocationResponse{}, nil
}

func (fakeInvoker) Stream(context.Context, InvocationRequest, StreamWriter) error { return nil }

func (fakeInvoker) Health(context.Context, TargetRef) HealthStatus {
	return HealthStatus{Healthy: true}
}

func (fakeInvoker) Capabilities(context.Context, TargetRef) CapabilitySet {
	return CapabilitySet{"chat": true}
}

func (fakeInvoker) NormalizeError(err error) *UpstreamError {
	return &UpstreamError{Code: "upstream_error", Message: err.Error()}
}

type fakeClock struct{ now time.Time }

func (c fakeClock) Now() time.Time { return c.now }

type fakeIDs struct{}

func (fakeIDs) New() (string, error) { return "id-1", nil }

func TestStableContracts(t *testing.T) {
	var invoker InteractionInvoker = fakeInvoker{}
	if _, err := invoker.Invoke(context.Background(), InvocationRequest{}); err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got := invoker.NormalizeError(errors.New("boom")); got.Code != "upstream_error" {
		t.Fatalf("NormalizeError() = %+v", got)
	}

	var clock Clock = fakeClock{now: time.Unix(10, 0)}
	if got := clock.Now(); !got.Equal(time.Unix(10, 0)) {
		t.Fatalf("Clock.Now() = %v", got)
	}

	var ids IDGenerator = fakeIDs{}
	if got, err := ids.New(); err != nil || got != "id-1" {
		t.Fatalf("IDGenerator.New() = %q", got)
	}
}
