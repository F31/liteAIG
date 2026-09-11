package coordination

import (
	"context"
	"errors"
	"testing"
	"time"
)

// RunBudgetConformance verifies atomic multi-window reserve, idempotent
// reconcile/release, and sweep behavior for any BudgetLedger implementation.
// It lives in the package (not a test-only subpackage) so both the in-memory
// and Redis implementations can share it without an import cycle.
func RunBudgetConformance(t *testing.T, ledger BudgetLedger) {
	t.Helper()
	ctx := context.Background()
	windows := []BudgetWindow{
		{Key: "tenant:1d", Limit: 100},
		{Key: "project:1d", Limit: 60},
	}

	t.Run("rejects when any window would be exceeded", func(t *testing.T) {
		if _, err := ledger.Reserve(ctx, ReserveRequest{TenantID: "t", ReservationID: "r-first", Estimate: 40, Windows: windows}); err != nil {
			t.Fatal(err)
		}
		result, err := ledger.Reserve(ctx, ReserveRequest{TenantID: "t", ReservationID: "r-big", Estimate: 50, Windows: windows})
		if err != nil {
			t.Fatal(err)
		}
		if result.Reserved {
			t.Fatal("reserve admitted a request that exceeds the project window")
		}
	})

	t.Run("reconciles exactly once", func(t *testing.T) {
		result, err := ledger.Reserve(ctx, ReserveRequest{TenantID: "t", ReservationID: "r-1", Estimate: 10, Windows: windows})
		if err != nil || !result.Reserved {
			t.Fatalf("reserve = %+v, %v", result, err)
		}
		if err := ledger.Reconcile(ctx, "t", "r-1", 7); err != nil {
			t.Fatal(err)
		}
		if err := ledger.Reconcile(ctx, "t", "r-1", 7); !errors.Is(err, ErrAlreadyFinalized) {
			t.Fatalf("duplicate reconcile = %v", err)
		}
	})

	t.Run("releases exactly once", func(t *testing.T) {
		result, err := ledger.Reserve(ctx, ReserveRequest{TenantID: "t", ReservationID: "r-2", Estimate: 5, Windows: windows})
		if err != nil || !result.Reserved {
			t.Fatalf("reserve = %+v, %v", result, err)
		}
		if err := ledger.Release(ctx, "t", "r-2"); err != nil {
			t.Fatal(err)
		}
		if err := ledger.Release(ctx, "t", "r-2"); !errors.Is(err, ErrAlreadyFinalized) {
			t.Fatalf("duplicate release = %v", err)
		}
	})

	t.Run("sweep is idempotent", func(t *testing.T) {
		if _, err := ledger.Sweep(ctx, "t", 500); err != nil {
			t.Fatal(err)
		}
		if _, err := ledger.Sweep(ctx, "t", 500); err != nil {
			t.Fatal(err)
		}
	})
}

// RunLeaseConformance verifies capacity enforcement, renew-only-existing,
// release, and cleanup for any LeaseCoordinator implementation.
func RunLeaseConformance(t *testing.T, coordinator LeaseCoordinator) {
	t.Helper()
	ctx := context.Background()

	t.Run("capacity enforced", func(t *testing.T) {
		first, id, err := coordinator.Acquire(ctx, "scope:1", 1, time.Hour)
		if err != nil || !first || id == "" {
			t.Fatalf("acquire = %t %q %v", first, id, err)
		}
		second, _, err := coordinator.Acquire(ctx, "scope:1", 1, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if second {
			t.Fatal("acquire admitted beyond capacity")
		}
		if err := coordinator.Release(ctx, "scope:1", id); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("renew only extends existing lease", func(t *testing.T) {
		ok, id, err := coordinator.Acquire(ctx, "scope:2", 1, time.Hour)
		if err != nil || !ok {
			t.Fatalf("acquire = %t %v", ok, err)
		}
		renewed, err := coordinator.Renew(ctx, "scope:2", id, time.Hour)
		if err != nil || !renewed {
			t.Fatalf("renew = %t %v", renewed, err)
		}
		missing, err := coordinator.Renew(ctx, "scope:2", "missing-lease", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if missing {
			t.Fatal("renew created a missing lease")
		}
		if err := coordinator.Release(ctx, "scope:2", id); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("cleanup is idempotent", func(t *testing.T) {
		if err := coordinator.Cleanup(ctx, "scope:3"); err != nil {
			t.Fatal(err)
		}
		if err := coordinator.Cleanup(ctx, "scope:3"); err != nil {
			t.Fatal(err)
		}
	})
}
