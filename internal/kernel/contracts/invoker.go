package contracts

import (
	"context"
	"time"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

// TargetRef identifies a compiled upstream target.
type TargetRef struct {
	Kind string
	ID   string
}

// InvocationRequest is the protocol-neutral input to a connector.
type InvocationRequest struct {
	Target                                                             TargetRef
	CredentialID                                                       string
	Request                                                            *interaction.UnifiedRequest
	TenantID, ProjectID, RequestID, SessionID, TaskID, AgentID, UserID string
}

// InvocationResponse is the protocol-neutral connector result.
type InvocationResponse struct {
	Response *interaction.UnifiedResponse
}

// StreamChunk is one normalized connector stream event.
type StreamChunk struct {
	Event interaction.StreamEvent
}

// StreamWriter receives normalized stream events and must honor context cancellation.
type StreamWriter interface {
	WriteChunk(context.Context, StreamChunk) error
}

// HealthStatus is a connector's bounded health observation.
type HealthStatus struct {
	Healthy   bool
	CheckedAt time.Time
	Reason    string
}

// CapabilitySet declares target features without provider-specific DTOs.
type CapabilitySet map[string]bool

// UpstreamError is a normalized connector failure.
type UpstreamError struct {
	Code         string
	StatusCode   int
	Retryable    bool
	Message      string
	ProviderCode string
	RetryAfter   time.Duration
}

func (e *UpstreamError) Error() string { return e.Code + ": " + e.Message }

// InteractionInvoker calls a target but never makes governance decisions.
type InteractionInvoker interface {
	Invoke(context.Context, InvocationRequest) (*InvocationResponse, error)
	Stream(context.Context, InvocationRequest, StreamWriter) error
	Health(context.Context, TargetRef) HealthStatus
	Capabilities(context.Context, TargetRef) CapabilitySet
	NormalizeError(error) *UpstreamError
}
