package coordination

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"
)

// RedisInflightCounter implements InflightCounter with Redis/Valkey counters.
// The scope key MUST contain a Cluster hash tag (e.g. "{tenant:<id>}:deployment:<id>").
type RedisInflightCounter struct {
	client redis.Cmdable
}

func NewRedisInflightCounter(client redis.Cmdable) *RedisInflightCounter {
	return &RedisInflightCounter{client: client}
}

func (r *RedisInflightCounter) key(scope string) string {
	return "inflight:" + scope + ":count"
}

var inflightDecrScript = redis.NewScript(`
-- KEYS[1] = inflight counter key
-- DECR but never below zero so a failed lease never leaves a permanent deficit.
local v = redis.call('DECR', KEYS[1])
if v < 0 then
  redis.call('SET', KEYS[1], 0)
  return 0
end
return v
`)

func (r *RedisInflightCounter) Incr(ctx context.Context, scope string) (int64, error) {
	if scope == "" {
		return 0, errors.New("inflight counter requires a scope")
	}
	return r.client.Incr(ctx, r.key(scope)).Result()
}

func (r *RedisInflightCounter) Decr(ctx context.Context, scope string) error {
	if scope == "" {
		return errors.New("inflight counter requires a scope")
	}
	_, err := inflightDecrScript.Run(ctx, r.client, []string{r.key(scope)}).Int64()
	return err
}

func (r *RedisInflightCounter) Get(ctx context.Context, scope string) (int64, error) {
	if scope == "" {
		return 0, errors.New("inflight counter requires a scope")
	}
	value, err := r.client.Get(ctx, r.key(scope)).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return value, err
}

var _ InflightCounter = (*RedisInflightCounter)(nil)
