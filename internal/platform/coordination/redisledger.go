package coordination

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisBudgetLedger implements BudgetLedger with atomic Lua scripts on Redis/Valkey.
// All window keys for one Tenant MUST share a Cluster hash tag (e.g. "budget:{tenant:<id>}:...").
type RedisBudgetLedger struct {
	client         redis.Cmdable
	clock          func() time.Time
	reservationTTL time.Duration
}

func NewRedisBudgetLedger(client redis.Cmdable, clock func() time.Time, reservationTTL time.Duration) *RedisBudgetLedger {
	if clock == nil {
		clock = time.Now
	}
	if reservationTTL <= 0 {
		reservationTTL = time.Hour
	}
	return &RedisBudgetLedger{client: client, clock: clock, reservationTTL: reservationTTL}
}

// BudgetKeys returns the reserved Redis key names for a tenant.
type budgetKeys struct {
	Reservation string
	Expiry      string
}

func (r *RedisBudgetLedger) keys(tenantID, reservationID string) budgetKeys {
	tag := "{tenant:" + tenantID + "}"
	return budgetKeys{
		Reservation: "budget:" + tag + ":reservation:" + reservationID,
		Expiry:      "budget:" + tag + ":reservation_expiry",
	}
}

var reserveScript = redis.NewScript(`
-- KEYS: [reservation, expiry, window1..windowN]
-- ARGV: [reservation_id, estimate, now_ms, expires_at_ms, ttl_ms, limit1..limitN, ttl1..ttlN]
local reservation = KEYS[1]
local expiry = KEYS[2]
local reservation_id = ARGV[1]
local estimate = tonumber(ARGV[2])
local expires_at = tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])
local n = #KEYS - 2
local arg = 5
for i=1,n do
  local limit = tonumber(ARGV[arg+i])
  local current = redis.call('GET', KEYS[2+i])
  if current and tonumber(current) + estimate > limit then
    return 0
  end
end
for i=1,n do
  local current = redis.call('GET', KEYS[2+i])
  local next = (current and tonumber(current) or 0) + estimate
  local ttl = tonumber(ARGV[arg+n+i])
  if ttl > 0 then
    redis.call('SET', KEYS[2+i], next, 'PX', ttl)
  else
    redis.call('SET', KEYS[2+i], next)
  end
end
redis.call('HSET', reservation, 'status', 'reserved', 'estimate', estimate, 'wc', n)
for i=1,n do
  redis.call('HSET', reservation, 'w'..(i-1), KEYS[2+i])
end
redis.call('ZADD', expiry, expires_at, reservation_id)
redis.call('PEXPIRE', reservation, ttl)
redis.call('PEXPIRE', expiry, ttl)
return 1
`)

func (r *RedisBudgetLedger) Reserve(ctx context.Context, req ReserveRequest) (ReserveResult, error) {
	if req.ReservationID == "" || req.Estimate < 0 || len(req.Windows) == 0 {
		return ReserveResult{}, errors.New("invalid reserve request")
	}
	keys := r.keys(req.TenantID, req.ReservationID)
	keyArgs := make([]string, 0, len(req.Windows)+2)
	keyArgs = append(keyArgs, keys.Reservation, keys.Expiry)
	values := make([]any, 0, 5+2*len(req.Windows))
	values = append(values, req.ReservationID, req.Estimate, r.clock().UnixMilli(), r.clock().Add(r.reservationTTL).UnixMilli(), r.reservationTTL.Milliseconds())
	for _, window := range req.Windows {
		if window.Key == "" || window.Limit < 0 {
			return ReserveResult{}, errors.New("invalid budget window")
		}
		keyArgs = append(keyArgs, window.Key)
		values = append(values, window.Limit)
	}
	for _, window := range req.Windows {
		values = append(values, window.TTL.Milliseconds())
	}
	result, err := reserveScript.Run(ctx, r.client, keyArgs, values...).Int()
	if err != nil {
		return ReserveResult{}, r.wrap(err)
	}
	return ReserveResult{Reserved: result == 1}, nil
}

var reconcileScript = redis.NewScript(`
-- KEYS: [reservation, expiry], ARGV: [reservation_id, actual]
local status = redis.call('HGET', KEYS[1], 'status')
if not status or status ~= 'reserved' then return 0 end
local estimate = tonumber(redis.call('HGET', KEYS[1], 'estimate') or 0)
local delta = tonumber(ARGV[2]) - estimate
local wc = tonumber(redis.call('HGET', KEYS[1], 'wc') or 0)
for i=0,wc-1 do
  local key = redis.call('HGET', KEYS[1], 'w'..i)
  if key then redis.call('INCRBYFLOAT', key, delta) end
end
redis.call('HSET', KEYS[1], 'status', 'reconciled')
redis.call('ZREM', KEYS[2], ARGV[1])
return 1
`)

func (r *RedisBudgetLedger) Reconcile(ctx context.Context, tenantID, reservationID string, actual float64) error {
	keys := r.keys(tenantID, reservationID)
	result, err := reconcileScript.Run(ctx, r.client, []string{keys.Reservation, keys.Expiry}, reservationID, actual).Int()
	if err != nil {
		return r.wrap(err)
	}
	if result != 1 {
		return ErrAlreadyFinalized
	}
	return nil
}

var releaseScript = redis.NewScript(`
-- KEYS: [reservation, expiry], ARGV: [reservation_id]
local status = redis.call('HGET', KEYS[1], 'status')
if not status or status ~= 'reserved' then return 0 end
local estimate = tonumber(redis.call('HGET', KEYS[1], 'estimate') or 0)
local wc = tonumber(redis.call('HGET', KEYS[1], 'wc') or 0)
for i=0,wc-1 do
  local key = redis.call('HGET', KEYS[1], 'w'..i)
  if key then redis.call('INCRBYFLOAT', key, -estimate) end
end
redis.call('HSET', KEYS[1], 'status', 'released')
redis.call('ZREM', KEYS[2], ARGV[1])
return 1
`)

func (r *RedisBudgetLedger) Release(ctx context.Context, tenantID, reservationID string) error {
	keys := r.keys(tenantID, reservationID)
	result, err := releaseScript.Run(ctx, r.client, []string{keys.Reservation, keys.Expiry}, reservationID).Int()
	if err != nil {
		return r.wrap(err)
	}
	if result != 1 {
		return ErrAlreadyFinalized
	}
	return nil
}

var expireScript = redis.NewScript(`
-- KEYS: [reservation, expiry], ARGV: [reservation_id]
redis.call('ZREM', KEYS[2], ARGV[1])
local status = redis.call('HGET', KEYS[1], 'status')
if not status then return 0 end
if status == 'reserved' then
  local estimate = tonumber(redis.call('HGET', KEYS[1], 'estimate') or 0)
  local wc = tonumber(redis.call('HGET', KEYS[1], 'wc') or 0)
  for i=0,wc-1 do
    local key = redis.call('HGET', KEYS[1], 'w'..i)
    if key then redis.call('INCRBYFLOAT', key, -estimate) end
  end
  redis.call('HSET', KEYS[1], 'status', 'expired')
end
return 1
`)

func (r *RedisBudgetLedger) Sweep(ctx context.Context, tenantID string, limit int) (int, error) {
	if limit <= 0 {
		limit = 500
	}
	expiryKey := r.keys(tenantID, "").Expiry
	now := strconv.FormatInt(r.clock().UnixMilli(), 10)
	swept := 0
	for swept < limit {
		ids, err := r.client.ZRangeByScore(ctx, expiryKey, &redis.ZRangeBy{
			Min: "-inf", Max: now, Count: int64(limit - swept), Offset: 0,
		}).Result()
		if err != nil {
			return swept, r.wrap(err)
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			keys := r.keys(tenantID, id)
			_, err := expireScript.Run(ctx, r.client, []string{keys.Reservation, keys.Expiry}, id).Int()
			if err != nil {
				return swept, r.wrap(err)
			}
			swept++
		}
		if len(ids) < limit-swept {
			break
		}
	}
	return swept, nil
}

func (r *RedisBudgetLedger) wrap(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errors.Join(ErrLedgerUnavailable, err)
}
