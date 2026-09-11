package coordination

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func newRedisLease(t *testing.T) *RedisLeaseCoordinator {
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
	return NewRedisLeaseCoordinator(client, clock)
}

func TestRedisLeaseConformance(t *testing.T) {
	coordinator := newRedisLease(t)
	RunLeaseConformance(t, coordinator)
}

func TestRedisLeaseExpiryRecoversCapacityWithoutRelease(t *testing.T) {
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
	coordinator := NewRedisLeaseCoordinator(client, func() time.Time { return now })
	ctx := context.Background()

	ok, _, err := coordinator.Acquire(ctx, "{tenant:t}:deployment:d", 1, time.Hour)
	if err != nil || !ok {
		t.Fatalf("acquire = %t %v", ok, err)
	}
	// Simulate process death: no Release. Advance the clock past expiry; the next
	// acquire prunes the expired member and restores capacity.
	now = now.Add(2 * time.Hour)
	second, _, err := coordinator.Acquire(ctx, "{tenant:t}:deployment:d", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !second {
		t.Fatal("expired lease from a dead process blocked capacity forever")
	}
}

func TestRedisLeaseConcurrentAcquireDoesNotExceedCapacity(t *testing.T) {
	coordinator := newRedisLease(t)
	ctx := context.Background()
	const capacity = 5
	var wait sync.WaitGroup
	results := make(chan bool, 200)
	for i := 0; i < 200; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ok, _, err := coordinator.Acquire(ctx, "{tenant:t}:deployment:d", capacity, time.Minute)
			if err != nil {
				results <- false
				return
			}
			results <- ok
		}()
	}
	wait.Wait()
	close(results)
	admitted := 0
	for ok := range results {
		if ok {
			admitted++
		}
	}
	if admitted != capacity {
		t.Fatalf("admitted = %d, want %d", admitted, capacity)
	}
}

func TestRedisLeaseCleanupRemovesExpired(t *testing.T) {
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
	coordinator := NewRedisLeaseCoordinator(client, func() time.Time { return now })
	ctx := context.Background()
	if ok, _, err := coordinator.Acquire(ctx, "{tenant:t}:deployment:d", 1, time.Hour); err != nil || !ok {
		t.Fatalf("acquire = %t %v", ok, err)
	}
	now = now.Add(2 * time.Hour)
	if err := coordinator.Cleanup(ctx, "{tenant:t}:deployment:d"); err != nil {
		t.Fatal(err)
	}
	ok, _, err := coordinator.Acquire(ctx, "{tenant:t}:deployment:d", 1, time.Hour)
	if err != nil || !ok {
		t.Fatalf("post-cleanup acquire = %t %v", ok, err)
	}
}
