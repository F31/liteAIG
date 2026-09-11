// Package kernel contains the stable, business-neutral gateway contracts.
package kernel

import (
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

// RequestContext is the strongly typed state shared by fixed pipeline stages.
// Snapshot is captured once during admission and must not be replaced in flight.
type RequestContext struct {
	RequestID            string
	ReceivedAt           time.Time
	Snapshot             *runtime.TenantRuntimeSnapshot
	Interaction          *interaction.Context
	Request              *interaction.UnifiedRequest
	Response             *interaction.UnifiedResponse
	Usage                *interaction.UnifiedUsage
	Source               string
	Key                  runtime.APIKey // authenticated principal credential (data plane)
	SelectedDeploymentID string
	ProviderCost         *float64
	Latency              time.Duration
	Err                  error                  // stage error captured before deferred accounting
	CacheHit             bool                   // response was served from the Exact Cache
	CachePending         bool                   // response should be written back to the Exact Cache
	CacheKey             string                 // scoped cache key for the current request
	StreamWriter         contracts.StreamWriter // optional live sink for streaming invocations
	BudgetPolicyID       string                 // budget policy reserved during preflight
	ReservationID        string                 // reservation to reconcile/release at finalize
	BudgetEstimated      int64                  // tokens reserved against the budget
	BudgetSoftWarning    bool                   // soft budget exceeded (degraded, not blocked)
	TPMEstimated         int64                  // tokens pre-reserved against the TPM meter
}

func (c *RequestContext) TenantID() string {
	if c.Interaction != nil {
		return c.Interaction.TenantID
	}
	return ""
}

func (c *RequestContext) ProjectID() string {
	if c.Interaction != nil {
		return c.Interaction.ProjectID
	}
	return ""
}

func (c *RequestContext) SnapshotVersion() int64 {
	if c.Snapshot != nil {
		return c.Snapshot.Version
	}
	return 0
}
