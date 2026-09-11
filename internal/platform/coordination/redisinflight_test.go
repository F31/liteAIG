package coordination

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"
)

func newRedisInflight(t *testing.T) *RedisInflightCounter {
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
	return NewRedisInflightCounter(client)
}

func TestRedisInflightIncrDecrGet(t *testing.T) {
	counter := newRedisInflight(t)
	ctx := context.Background()
	const scope = "{tenant:t}:deployment:d"

	first, err := counter.Incr(ctx, scope)
	if err != nil || first != 1 {
		t.Fatalf("incr = %d %v", first, err)
	}
	second, err := counter.Incr(ctx, scope)
	if err != nil || second != 2 {
		t.Fatalf("incr = %d %v", second, err)
	}
	if err := counter.Decr(ctx, scope); err != nil {
		t.Fatal(err)
	}
	current, err := counter.Get(ctx, scope)
	if err != nil || current != 1 {
		t.Fatalf("get = %d %v", current, err)
	}
}

func TestRedisInflightDecrNeverGoesNegative(t *testing.T) {
	counter := newRedisInflight(t)
	ctx := context.Background()
	const scope = "{tenant:t}:deployment:d"

	// A failed lease must never leave a permanent deficit after over-decrementing.
	for i := 0; i < 5; i++ {
		if err := counter.Decr(ctx, scope); err != nil {
			t.Fatal(err)
		}
	}
	current, err := counter.Get(ctx, scope)
	if err != nil || current != 0 {
		t.Fatalf("get = %d %v", current, err)
	}
}

func TestRedisInflightCopiesAcrossLeases(t *testing.T) {
	addr := os.Getenv("LITEAIG_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("LITEAIG_TEST_REDIS_ADDR is not configured")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	counter := NewRedisInflightCounter(client)
	ctx := context.Background()
	const scope = "{tenant:t}:deployment:d"

	// Two independent counter instances over the same client share the count,
	// proving the value lives in Redis, not in the process.
	other := NewRedisInflightCounter(client)
	if _, err := counter.Incr(ctx, scope); err != nil {
		t.Fatal(err)
	}
	value, err := other.Get(ctx, scope)
	if err != nil || value != 1 {
		t.Fatalf("other.Get = %d %v", value, err)
	}
}

func TestRedisInflightConcurrentIncr(t *testing.T) {
	counter := newRedisInflight(t)
	ctx := context.Background()
	const scope = "{tenant:t}:deployment:d"
	const workers = 20
	var wait sync.WaitGroup
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _ = counter.Incr(ctx, scope)
		}()
	}
	wait.Wait()
	value, err := counter.Get(ctx, scope)
	if err != nil || value != workers {
		t.Fatalf("get = %d %v", value, err)
	}
}
