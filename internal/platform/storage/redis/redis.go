// Package redis provides the Standard/Enterprise Redis/Valkey coordination adapter.
package redis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Config carries the Redis connection settings for coordination primitives.
type Config struct {
	Addr     string
	Password string
	DB       int
}

// Open creates a go-redis client for the coordination layer.
func Open(ctx context.Context, config Config) (*redis.Client, error) {
	if config.Addr == "" {
		return nil, fmt.Errorf("redis addr is required")
	}
	client := redis.NewClient(&redis.Options{
		Addr:     config.Addr,
		Password: config.Password,
		DB:       config.DB,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}
