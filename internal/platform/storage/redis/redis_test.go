package redis

import (
	"context"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
)

func testClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("LITEAIG_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("LITEAIG_TEST_REDIS_ADDR is not configured")
	}
	client, err := Open(context.Background(), Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestRedisConnectivity(t *testing.T) {
	client := testClient(t)
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := client.Set(context.Background(), "liteaig:conformance:probe", "ok", 0).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}
}
