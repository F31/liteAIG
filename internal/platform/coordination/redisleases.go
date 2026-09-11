package coordination

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLeaseCoordinator implements LeaseCoordinator with Redis/Valkey ZSET leases.
// The scope key MUST contain a Cluster hash tag (e.g. "{tenant:<id>}:deployment:<id>").
type RedisLeaseCoordinator struct {
	client redis.Cmdable
	clock  func() time.Time
}

func NewRedisLeaseCoordinator(client redis.Cmdable, clock func() time.Time) *RedisLeaseCoordinator {
	if clock == nil {
		clock = time.Now
	}
	return &RedisLeaseCoordinator{client: client, clock: clock}
}

func (r *RedisLeaseCoordinator) key(scope string) string {
	return "concurrency:" + scope + ":leases"
}

var acquireScript = redis.NewScript(`
-- KEYS[1] = concurrency:{scope}:leases
-- ARGV[1] = lease_id, ARGV[2] = capacity, ARGV[3] = now_ms, ARGV[4] = expires_at_ms
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[3])
local active = redis.call('ZCARD', KEYS[1])
if active >= tonumber(ARGV[2]) then
  return {0, active}
end
redis.call('ZADD', KEYS[1], ARGV[4], ARGV[1])
local ttl = math.max(60000, tonumber(ARGV[4]) - tonumber(ARGV[3]) + 60000)
redis.call('PEXPIRE', KEYS[1], ttl)
return {1, active + 1}
`)

func (r *RedisLeaseCoordinator) Acquire(ctx context.Context, scope string, capacity int, ttl time.Duration) (bool, string, error) {
	if scope == "" || capacity <= 0 || ttl <= 0 {
		return false, "", nil
	}
	leaseID, err := newLeaseID()
	if err != nil {
		return false, "", err
	}
	now := r.clock().UnixMilli()
	expiresAt := r.clock().Add(ttl).UnixMilli()
	result, err := acquireScript.Run(ctx, r.client, []string{r.key(scope)}, leaseID, capacity, now, expiresAt).Int64Slice()
	if err != nil {
		return false, "", err
	}
	if len(result) == 0 || result[0] != 1 {
		return false, "", nil
	}
	return true, leaseID, nil
}

var renewScript = redis.NewScript(`
-- KEYS[1] = leases key, ARGV[1] = lease_id, ARGV[2] = expires_at_ms, ARGV[3] = now_ms
if redis.call('ZSCORE', KEYS[1], ARGV[1]) then
  redis.call('ZADD', KEYS[1], 'XX', ARGV[2], ARGV[1])
  redis.call('PEXPIRE', KEYS[1], math.max(60000, tonumber(ARGV[2]) - tonumber(ARGV[3]) + 60000))
  return 1
end
return 0
`)

func (r *RedisLeaseCoordinator) Renew(ctx context.Context, scope, leaseID string, ttl time.Duration) (bool, error) {
	if scope == "" || leaseID == "" || ttl <= 0 {
		return false, nil
	}
	now := r.clock().UnixMilli()
	expiresAt := r.clock().Add(ttl).UnixMilli()
	// PEXPIRE needs the absolute expiry, so the script receives expires_at and derives TTL.
	result, err := renewScript.Run(ctx, r.client, []string{r.key(scope)}, leaseID, expiresAt, now).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

var leaseReleaseScript = redis.NewScript(`
-- KEYS[1] = leases key, ARGV[1] = lease_id
return redis.call('ZREM', KEYS[1], ARGV[1])
`)

func (r *RedisLeaseCoordinator) Release(ctx context.Context, scope, leaseID string) error {
	if scope == "" || leaseID == "" {
		return nil
	}
	_, err := leaseReleaseScript.Run(ctx, r.client, []string{r.key(scope)}, leaseID).Int()
	return err
}

func (r *RedisLeaseCoordinator) Cleanup(ctx context.Context, scope string) error {
	if scope == "" {
		return nil
	}
	now := strconv.FormatInt(r.clock().UnixMilli(), 10)
	return r.client.ZRemRangeByScore(ctx, r.key(scope), "-inf", now).Err()
}

var _ LeaseCoordinator = (*RedisLeaseCoordinator)(nil)

// leaseKeyTTL returns the bounded key TTL used by acquire/renew (kept here for docs).
func leaseKeyTTL(expiresAt, now int64) int64 {
	return int64(math.Max(60000, float64(expiresAt-now+60000)))
}
