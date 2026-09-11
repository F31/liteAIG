package guardrail

import (
	"context"
	"errors"
)

type ExternalFailMode string

const (
	ExternalFailOpen   ExternalFailMode = "fail_open"
	ExternalFailClosed ExternalFailMode = "fail_closed"
	ExternalBypass     ExternalFailMode = "bypass"
)

var ErrExternalUnavailable = errors.New("external guardrail unavailable")

type ExternalRequest struct{ TenantID, ProjectID, RequestID, ContentHash string }
type ExternalResult struct {
	Blocked, Degraded bool
	Reason            string
}

type ExternalProvider interface {
	Evaluate(context.Context, ExternalRequest) (ExternalResult, error)
}
type ExternalObserver interface{ RecordExternalFailure(ExternalFailMode) }

func EvaluateExternal(ctx context.Context, provider ExternalProvider, request ExternalRequest, mode ExternalFailMode, observer ExternalObserver) (ExternalResult, error) {
	if mode == ExternalBypass {
		return ExternalResult{Degraded: true, Reason: "bypassed"}, nil
	}
	result, err := provider.Evaluate(ctx, request)
	if err == nil {
		return result, nil
	}
	if observer != nil {
		observer.RecordExternalFailure(mode)
	}
	if mode == ExternalFailOpen {
		return ExternalResult{Degraded: true, Reason: "provider_unavailable"}, nil
	}
	return ExternalResult{}, ErrExternalUnavailable
}
