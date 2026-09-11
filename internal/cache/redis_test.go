package cache

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisStoreContract(t *testing.T) {
	addr := os.Getenv("LITEAIG_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("redis not configured")
	}
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	store := NewRedisStore(client, time.Minute)
	key := "cache:test:" + time.Now().Format("150405.000000000")
	defer client.Del(ctx, key)

	if _, err := store.Get(ctx, key); !errors.Is(err, ErrMiss) {
		t.Fatalf("initial Get() error = %v", err)
	}
	entry := &Entry{Response: []byte(`{"id":"cached"}`), StoredAt: time.Now(), TTL: time.Minute}
	if err := store.Put(ctx, key, entry); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, key)
	if err != nil || string(got.Response) != string(entry.Response) {
		t.Fatalf("Get() = %+v, %v", got, err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, key); !errors.Is(err, ErrMiss) {
		t.Fatalf("Get() after Delete error = %v", err)
	}
}
