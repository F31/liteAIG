package leasegate

import (
	"context"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/platform/coordination"
)

type alwaysOpen struct{}

func (alwaysOpen) Allow(string, string) bool { return false }

type halfOpen struct{ allowed bool }

func (c *halfOpen) Allow(string, string) bool {
	if !c.allowed {
		c.allowed = true
		return true
	}
	return false
}

type alwaysClosed struct{}

func (alwaysClosed) Allow(string, string) bool { return true }

type recordingCoordinator struct {
	acquires int
}

func (r *recordingCoordinator) Acquire(context.Context, string, int, time.Duration) (bool, string, error) {
	r.acquires++
	return true, "lease-1", nil
}
func (r *recordingCoordinator) Renew(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}
func (r *recordingCoordinator) Release(context.Context, string, string) error { return nil }
func (r *recordingCoordinator) Cleanup(context.Context, string) error         { return nil }

func TestOpenCircuitSkipsLease(t *testing.T) {
	coordinator := &recordingCoordinator{}
	gate := New(coordinator, alwaysOpen{})
	ok, id, err := gate.Acquire(context.Background(), "{tenant:t}:d", "d", "c", 4, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if ok || id != "" {
		t.Fatalf("acquire = %t %q", ok, id)
	}
	if coordinator.acquires != 0 {
		t.Fatalf("open circuit still acquired a lease: %d", coordinator.acquires)
	}
}

func TestHalfOpenProbeStillLeases(t *testing.T) {
	coordinator := &recordingCoordinator{}
	gate := New(coordinator, &halfOpen{})
	ok, _, err := gate.Acquire(context.Background(), "{tenant:t}:d", "d", "c", 4, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("half-open probe did not acquire a lease")
	}
	if coordinator.acquires != 1 {
		t.Fatalf("probe acquires = %d", coordinator.acquires)
	}
}

func TestClosedCircuitAcquires(t *testing.T) {
	coordinator := &recordingCoordinator{}
	gate := New(coordinator, alwaysClosed{})
	ok, _, err := gate.Acquire(context.Background(), "{tenant:t}:d", "d", "c", 4, time.Hour)
	if err != nil || !ok {
		t.Fatalf("acquire = %t %v", ok, err)
	}
}

var _ coordination.LeaseCoordinator = (*recordingCoordinator)(nil)
