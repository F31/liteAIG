package rate

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLimiter enforces the RPM token bucket across gateway replicas. The scope
// key carries a tenant hash tag so all keys for one tenant stay in the same
// Redis Cluster slot.
type RedisLimiter struct {
	client redis.Cmdable
	clock  func() time.Time
}

// NewRedisLimiter builds a shared RPM limiter. A nil clock uses time.Now.
func NewRedisLimiter(client redis.Cmdable, clock func() time.Time) *RedisLimiter {
	if clock == nil {
		clock = time.Now
	}
	return &RedisLimiter{client: client, clock: clock}
}

func (l *RedisLimiter) key(tenantID, projectID string) string {
	return "rate:{tenant:" + tenantID + "}:" + projectID
}

var rpmScript = redis.NewScript(`
-- KEYS[1] = scope key
-- ARGV[1] = capacity, ARGV[2] = requests/minute, ARGV[3] = now_ms
local capacity = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local values = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local tokens = tonumber(values[1])
local ts = tonumber(values[2])
if not tokens then
  tokens = capacity
  ts = now
end
local elapsed = math.max(0, now - ts) / 60000
tokens = math.min(capacity, tokens + elapsed * rate)
local allowed = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
end
redis.call('HSET', KEYS[1], 'tokens', tokens, 'ts', now)
redis.call('PEXPIRE', KEYS[1], 120000)
return {tostring(allowed), tostring(tokens)}
`)

// Allow implements RPM with a Redis-backed token bucket.
func (l *RedisLimiter) Allow(tenantID, projectID string, policy Policy) Decision {
	if policy.RequestsPerMinute <= 0 || policy.Burst <= 0 {
		return Decision{Allowed: false, RetryAfter: time.Minute}
	}
	result, err := rpmScript.Run(context.Background(), l.client,
		[]string{l.key(tenantID, projectID)},
		policy.Burst, policy.RequestsPerMinute, l.clock().UnixMilli()).StringSlice()
	if err != nil || len(result) != 2 {
		// Fail open on a coordinator outage: local budgets/leases still bound
		// total spend, and a hard failure here would take down all traffic.
		return Decision{Allowed: true}
	}
	if result[0] == "1" {
		return Decision{Allowed: true}
	}
	tokens, _ := strconv.ParseFloat(result[1], 64)
	missing := math.Max(0, 1-tokens)
	retry := time.Duration(missing / float64(policy.RequestsPerMinute) * float64(time.Minute))
	if retry <= 0 {
		retry = time.Second
	}
	return Decision{Allowed: false, RetryAfter: retry}
}

// RedisMeter enforces TPM in a fixed one-minute window across replicas.
type RedisMeter struct {
	client redis.Cmdable
	clock  func() time.Time
	policy TPMPolicyFunc
}

// NewRedisMeter builds a shared TPM meter.
func NewRedisMeter(client redis.Cmdable, clock func() time.Time, policy TPMPolicyFunc) *RedisMeter {
	if clock == nil {
		clock = time.Now
	}
	return &RedisMeter{client: client, clock: clock, policy: policy}
}

func (m *RedisMeter) key(tenantID, projectID string, now time.Time) string {
	return "tpm:{tenant:" + tenantID + "}:" + projectID + ":" + strconv.FormatInt(now.Unix()/60, 10)
}

var tpmReserveScript = redis.NewScript(`
-- KEYS[1] = minute window; ARGV[1] = estimate, ARGV[2] = limit
local estimate = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local used = tonumber(redis.call('GET', KEYS[1]) or '0')
if used + estimate > limit then
  return 0
end
redis.call('INCRBY', KEYS[1], estimate)
redis.call('PEXPIRE', KEYS[1], 120000)
return 1
`)

// Reserve pre-reserves estimated tokens in the shared window.
func (m *RedisMeter) Reserve(tenantID, projectID string, estimated int64) (TPMDecision, int) {
	limit := int64(0)
	if m.policy != nil {
		limit = m.policy(tenantID, projectID)
	}
	if limit <= 0 || estimated <= 0 {
		return TPMDecision{Allowed: true}, -1
	}
	now := m.clock()
	result, err := tpmReserveScript.Run(context.Background(), m.client,
		[]string{m.key(tenantID, projectID, now)}, estimated, limit).Int()
	if err != nil {
		return TPMDecision{Allowed: true}, -1
	}
	if result == 1 {
		return TPMDecision{Allowed: true}, 0
	}
	return TPMDecision{Allowed: false, RetryAfter: now.Truncate(time.Minute).Add(time.Minute).Sub(now)}, -1
}

var tpmReconcileScript = redis.NewScript(`
local delta = tonumber(ARGV[1])
local used = tonumber(redis.call('GET', KEYS[1]) or '0')
local next = used + delta
if next < 0 then next = 0 end
redis.call('SET', KEYS[1], next, 'PX', 120000)
return 1
`)

// Reconcile adjusts the reserved estimate to actual usage.
func (m *RedisMeter) Reconcile(tenantID, projectID string, estimated, actual int64) {
	if estimated <= 0 && actual <= 0 {
		return
	}
	delta := actual - estimated
	if delta == 0 {
		return
	}
	_, _ = tpmReconcileScript.Run(context.Background(), m.client,
		[]string{m.key(tenantID, projectID, m.clock())}, delta).Int()
}

var _ RPM = (*RedisLimiter)(nil)
var _ TPM = (*RedisMeter)(nil)
