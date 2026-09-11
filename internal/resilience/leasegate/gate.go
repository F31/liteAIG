// Package leasegate couples lease acquisition to circuit state so open circuits
// never consume capacity while half-open probes still acquire a lease.
package leasegate

import (
	"context"
	"time"

	"github.com/F31/liteAIG/internal/platform/coordination"
)

// CircuitView is the minimal circuit decision surface for leasing.
type CircuitView interface {
	Allow(deploymentID, credentialID string) bool
}

// Gate acquires a lease only when the circuit admits a call. Open circuits skip
// leasing entirely; the single half-open probe is still admitted and leased.
type Gate struct {
	coordinator coordination.LeaseCoordinator
	circuit     CircuitView
}

func New(coordinator coordination.LeaseCoordinator, circuit CircuitView) *Gate {
	return &Gate{coordinator: coordinator, circuit: circuit}
}

// Acquire returns busy=false when the circuit is open or capacity is full.
func (g *Gate) Acquire(ctx context.Context, scope, deploymentID, credentialID string, capacity int, ttl time.Duration) (bool, string, error) {
	if g.circuit != nil && !g.circuit.Allow(deploymentID, credentialID) {
		return false, "", nil
	}
	return g.coordinator.Acquire(ctx, scope, capacity, ttl)
}

func (g *Gate) Renew(ctx context.Context, scope, leaseID string, ttl time.Duration) (bool, error) {
	return g.coordinator.Renew(ctx, scope, leaseID, ttl)
}

func (g *Gate) Release(ctx context.Context, scope, leaseID string) error {
	return g.coordinator.Release(ctx, scope, leaseID)
}
