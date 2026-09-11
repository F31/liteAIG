package rate

import (
	"sync"
	"testing"
	"time"
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }
func TestScopedLimiterAndRefill(t *testing.T) {
	clock := &clock{now: time.Unix(1, 0)}
	limiter := New(clock)
	policy := Policy{RequestsPerMinute: 60, Burst: 2}
	if !limiter.Allow("a", "p", policy).Allowed || !limiter.Allow("a", "p", policy).Allowed {
		t.Fatal("initial burst denied")
	}
	denied := limiter.Allow("a", "p", policy)
	if denied.Allowed || denied.RetryAfter <= 0 {
		t.Fatalf("decision=%+v", denied)
	}
	if !limiter.Allow("b", "p", policy).Allowed {
		t.Fatal("tenant scopes shared a bucket")
	}
	clock.Add(time.Second)
	if !limiter.Allow("a", "p", policy).Allowed {
		t.Fatal("bucket did not refill")
	}
}
func TestIdleBucketsAreEvicted(t *testing.T) {
	clock := &clock{now: time.Unix(1, 0)}
	limiter := New(clock)
	policy := Policy{RequestsPerMinute: 60, Burst: 2}
	if !limiter.Allow("idle", "p", policy).Allowed {
		t.Fatal("initial allow failed")
	}
	// Leave the bucket idle past both the sweep interval and the idle TTL.
	clock.Add(sweepInterval + idleTTL + time.Minute)
	if !limiter.Allow("busy", "p", policy).Allowed {
		t.Fatal("busy scope allow failed")
	}
	if limiter.bucketExists("idle:p") {
		t.Fatal("idle bucket was not evicted")
	}
	if !limiter.bucketExists("busy:p") {
		t.Fatal("active bucket was evicted")
	}
}
func TestLimiterConcurrentDoesNotExceedBurst(t *testing.T) {
	clock := &clock{now: time.Unix(1, 0)}
	limiter := New(clock)
	var wait sync.WaitGroup
	allowed := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			allowed <- limiter.Allow("a", "p", Policy{RequestsPerMinute: 1, Burst: 10}).Allowed
		}()
	}
	wait.Wait()
	close(allowed)
	count := 0
	for value := range allowed {
		if value {
			count++
		}
	}
	if count != 10 {
		t.Fatalf("allowed=%d", count)
	}
}

func TestMeterReserveAndReconcile(t *testing.T) {
	clock := &clock{now: time.Unix(1, 0)}
	policy := func(_, _ string) int64 { return 100 }
	meter := NewMeter(clock, policy)
	// Two concurrent estimates fit the window.
	if d, _ := meter.Reserve("a", "p", 60); !d.Allowed {
		t.Fatalf("first reserve denied: %+v", d)
	}
	if d, _ := meter.Reserve("a", "p", 40); !d.Allowed {
		t.Fatalf("second reserve denied: %+v", d)
	}
	// A third estimate would exceed the 100-token window.
	if d, _ := meter.Reserve("a", "p", 10); d.Allowed {
		t.Fatal("over-limit reserve was allowed")
	} else if d.RetryAfter <= 0 {
		t.Fatalf("expected a positive retry window: %+v", d)
	}
	// Reconcile the two reservations down to their actual usage.
	meter.Reconcile("a", "p", 60, 55)
	meter.Reconcile("a", "p", 40, 0)
	if _, used, limit, active := meter.windowState("a", "p"); !active {
		t.Fatal("window not active")
	} else if used != 55 || limit != 100 {
		t.Fatalf("used=%d limit=%d", used, limit)
	}
	// After reconcile, 45 tokens remain: a 10-token request now fits.
	if d, _ := meter.Reserve("a", "p", 10); !d.Allowed {
		t.Fatalf("reconcile did not free tokens: %+v", d)
	}
}

func TestMeterWindowRollsOver(t *testing.T) {
	clock := &clock{now: time.Unix(1, 0)}
	policy := func(_, _ string) int64 { return 100 }
	meter := NewMeter(clock, policy)
	if d, _ := meter.Reserve("a", "p", 100); !d.Allowed {
		t.Fatal("full-window reserve denied")
	}
	if d, _ := meter.Reserve("a", "p", 1); d.Allowed {
		t.Fatal("second reserve in same window was allowed")
	}
	// Roll into the next minute: the ledger resets.
	clock.Add(time.Minute)
	if d, _ := meter.Reserve("a", "p", 100); !d.Allowed {
		t.Fatalf("rollover reserve denied: %+v", d)
	}
}

func TestMeterDisabledPolicy(t *testing.T) {
	clock := &clock{now: time.Unix(1, 0)}
	meter := NewMeter(clock, func(_, _ string) int64 { return 0 })
	if d, _ := meter.Reserve("a", "p", 1000); !d.Allowed {
		t.Fatal("zero policy must allow everything")
	}
}

func TestMeterScopesAreIndependent(t *testing.T) {
	clock := &clock{now: time.Unix(1, 0)}
	policy := func(_, _ string) int64 { return 10 }
	meter := NewMeter(clock, policy)
	if d, _ := meter.Reserve("a", "p", 10); !d.Allowed {
		t.Fatal("scope a deny")
	}
	if d, _ := meter.Reserve("b", "p", 10); !d.Allowed {
		t.Fatal("scope b must have its own ledger")
	}
}
