package soak

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/platform/coordination"
	"github.com/redis/go-redis/v9"
)

func TestPhase1SoakHarness(t *testing.T) {
	duration := 250 * time.Millisecond
	if configured := os.Getenv("LITEAIG_SOAK_DURATION"); configured != "" {
		parsed, err := time.ParseDuration(configured)
		if err != nil {
			t.Fatal(err)
		}
		duration = parsed
	}
	interval := time.Millisecond
	if configured := os.Getenv("LITEAIG_SOAK_INTERVAL"); configured != "" {
		parsed, err := time.ParseDuration(configured)
		if err != nil || parsed <= 0 {
			t.Fatalf("invalid LITEAIG_SOAK_INTERVAL %q: must be a positive duration", configured)
		}
		interval = parsed
	}
	var ledger coordination.BudgetLedger
	var leases coordination.LeaseCoordinator
	if addr := os.Getenv("LITEAIG_TEST_REDIS_ADDR"); addr != "" {
		client := redis.NewClient(&redis.Options{Addr: addr})
		defer client.Close()
		if err := client.FlushDB(context.Background()).Err(); err != nil {
			t.Fatal(err)
		}
		ledger = coordination.NewRedisBudgetLedger(client, nil, time.Minute)
		leases = coordination.NewRedisLeaseCoordinator(client, nil)
	} else {
		ledger = coordination.NewMemoryBudgetLedger(nil)
		leases = coordination.NewMemoryLeaseCoordinator(nil)
	}
	iterations := runHarness(t, duration, interval, ledger, leases)
	if iterations == 0 {
		t.Fatal("soak harness ran no iterations")
	}
}

func runHarness(t *testing.T, duration, interval time.Duration, ledger coordination.BudgetLedger, leases coordination.LeaseCoordinator) int {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(duration)
	iterations := 0
	windows := []coordination.BudgetWindow{{Key: "budget:{tenant:soak}:tenant:1d", Limit: 1000, TTL: time.Hour}}
	for time.Now().Before(deadline) {
		id := time.Now().Format("150405.000000000")
		result, err := ledger.Reserve(ctx, coordination.ReserveRequest{TenantID: "soak", ReservationID: id, Estimate: 1, Windows: windows})
		if err != nil || !result.Reserved {
			t.Fatalf("reserve iteration %d: %+v %v", iterations, result, err)
		}
		if err := ledger.Reconcile(ctx, "soak", id, 0); err != nil {
			t.Fatal(err)
		}
		ok, leaseID, err := leases.Acquire(ctx, "{tenant:soak}:deployment:d", 4, 50*time.Millisecond)
		if err != nil || !ok {
			t.Fatalf("lease iteration %d: %t %v", iterations, ok, err)
		}
		if err := leases.Release(ctx, "{tenant:soak}:deployment:d", leaseID); err != nil {
			t.Fatal(err)
		}
		iterations++
		time.Sleep(interval)
	}
	// No reservation leak: the full window remains available after zero-actual reconciles.
	result, err := ledger.Reserve(ctx, coordination.ReserveRequest{TenantID: "soak", ReservationID: "final", Estimate: 1000, Windows: windows})
	if err != nil || !result.Reserved {
		t.Fatalf("reservation leak after %d iterations: %+v %v", iterations, result, err)
	}
	// No lease leak: all capacity is available.
	var ids []string
	for i := 0; i < 4; i++ {
		ok, id, err := leases.Acquire(ctx, "{tenant:soak}:deployment:d", 4, time.Second)
		if err != nil || !ok {
			t.Fatalf("lease leak at slot %d: %t %v", i, ok, err)
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		_ = leases.Release(ctx, "{tenant:soak}:deployment:d", id)
	}
	return iterations
}
