package coordination

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func newRedisLedger(t *testing.T, tenantID string) (*redis.Client, *RedisBudgetLedger) {
	t.Helper()
	addr := os.Getenv("LITEAIG_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("LITEAIG_TEST_REDIS_ADDR is not configured")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1, 0)
	clock := func() time.Time { return now }
	return client, NewRedisBudgetLedger(client, clock, time.Hour)
}

func TestRedisBudgetLedgerConformance(t *testing.T) {
	_, ledger := newRedisLedger(t, "t")
	RunBudgetConformance(t, ledger)
}

func TestRedisBudgetLedgerAtomicRejection(t *testing.T) {
	_, ledger := newRedisLedger(t, "t")
	ctx := context.Background()
	windows := []BudgetWindow{{Key: "{tenant:t}:tenant:1d", Limit: 100}, {Key: "{tenant:t}:project:p:1d", Limit: 50}}
	if _, err := ledger.Reserve(ctx, ReserveRequest{TenantID: "t", ReservationID: "a", Estimate: 30, Windows: windows}); err != nil {
		t.Fatal(err)
	}
	second, err := ledger.Reserve(ctx, ReserveRequest{TenantID: "t", ReservationID: "b", Estimate: 30, Windows: windows})
	if err != nil {
		t.Fatal(err)
	}
	if second.Reserved {
		t.Fatal("atomic reserve admitted when project window is exhausted")
	}
	if err := ledger.Release(ctx, "t", "a"); err != nil {
		t.Fatal(err)
	}
}

func TestRedisBudgetLedgerSweepRecoversAbandonedReservation(t *testing.T) {
	_, ledger := newRedisLedger(t, "sweep")
	ctx := context.Background()
	windows := []BudgetWindow{{Key: "{tenant:sweep}:tenant:1d", Limit: 100}}
	if _, err := ledger.Reserve(ctx, ReserveRequest{TenantID: "sweep", ReservationID: "abandoned", Estimate: 40, Windows: windows}); err != nil {
		t.Fatal(err)
	}
	// Reservation expiry uses the ledger clock; advance it past the reservation TTL.
	ledger.clock = func() time.Time { return time.Unix(1, 0).Add(2 * time.Hour) }
	swept, err := ledger.Sweep(ctx, "sweep", 100)
	if err != nil {
		t.Fatal(err)
	}
	if swept != 1 {
		t.Fatalf("swept = %d", swept)
	}
	// The estimate must be released so a new reservation fits.
	result, err := ledger.Reserve(ctx, ReserveRequest{TenantID: "sweep", ReservationID: "after-sweep", Estimate: 100, Windows: windows})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Reserved {
		t.Fatal("sweep did not release the abandoned estimate")
	}
}
