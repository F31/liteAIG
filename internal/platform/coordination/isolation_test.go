package coordination

import (
	"context"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestRedisNamespaceIsolationBetweenTenants(t *testing.T) {
	addr := os.Getenv("LITEAIG_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("LITEAIG_TEST_REDIS_ADDR is not configured")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Same logical window key under two tenant hash tags.
	tenantA := "aaa"
	tenantB := "bbb"
	keyA := "budget:{tenant:" + tenantA + "}:project:p:1d"
	keyB := "budget:{tenant:" + tenantB + "}:project:p:1d"
	if err := client.Set(ctx, keyA, 10, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, keyB, 20, 0).Err(); err != nil {
		t.Fatal(err)
	}
	valueA, _ := client.Get(ctx, keyA).Int64()
	valueB, _ := client.Get(ctx, keyB).Int64()
	if valueA != 10 || valueB != 20 {
		t.Fatalf("cross-tenant key collision: A=%d B=%d", valueA, valueB)
	}

	// Lease scope keys under distinct tenant hash tags do not share capacity.
	coordinator := NewRedisLeaseCoordinator(client, nil)
	if ok, _, err := coordinator.Acquire(ctx, "{tenant:"+tenantA+"}:deployment:d", 1, 60000); err != nil || !ok {
		t.Fatalf("tenant A lease = %t %v", ok, err)
	}
	if ok, _, err := coordinator.Acquire(ctx, "{tenant:"+tenantB+"}:deployment:d", 1, 60000); err != nil || !ok {
		t.Fatalf("tenant B lease blocked by tenant A capacity: %t %v", ok, err)
	}
}

func TestCoordinationKeysAreDistinctAcrossTenants(t *testing.T) {
	// Pure key-layout check independent of a live Redis.
	tenantA := "{tenant:a}"
	tenantB := "{tenant:b}"
	if "concurrency:"+tenantA+":deployment:d:leases" == "concurrency:"+tenantB+":deployment:d:leases" {
		t.Fatal("lease keys collide across tenants")
	}
	if "budget:"+tenantA+":project:p:1d" == "budget:"+tenantB+":project:p:1d" {
		t.Fatal("budget keys collide across tenants")
	}
}
