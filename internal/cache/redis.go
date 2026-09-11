package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore is a Valkey/Redis-backed cache (Standard/Enterprise tier). Keys are
// tenant/project-scoped by construction via Key.String.
type RedisStore struct {
	client redis.Cmdable
	ttl    time.Duration
}

// NewRedisStore builds a cache over an existing Redis client.
func NewRedisStore(client redis.Cmdable, ttl time.Duration) *RedisStore {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &RedisStore{client: client, ttl: ttl}
}

// Get returns a live entry or ErrMiss.
func (s *RedisStore) Get(ctx context.Context, key string) (*Entry, error) {
	raw, err := s.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, ErrMiss
	}
	if err != nil {
		return nil, err
	}
	var entry Entry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// Put stores a serialized entry with the store TTL.
func (s *RedisStore) Put(ctx context.Context, key string, entry *Entry) error {
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	ttl := entry.TTL
	if ttl <= 0 {
		ttl = s.ttl
	}
	return s.client.Set(ctx, key, raw, ttl).Err()
}

// Delete removes an entry.
func (s *RedisStore) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, key).Err()
}

var _ Store = (*RedisStore)(nil)
