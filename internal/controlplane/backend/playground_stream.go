package backend

import (
	"context"

	"github.com/F31/liteAIG/internal/tenancy"
)

// StreamEventView is one normalized playground stream token exposed to the
// Console SSE client.
type StreamEventView struct {
	Delta      string `json:"delta"`
	StopReason string `json:"stopReason,omitempty"`
	Final      bool   `json:"final"`
}

// PlaygroundStreamer is implemented by backends that can stream playground
// invocations as live events (SSE framing is owned by the server layer).
type PlaygroundStreamer interface {
	PlaygroundStream(ctx context.Context, scope tenancy.TenantScope, input PlaygroundRequest, emit func(StreamEventView)) (PlaygroundResponse, error)
}
