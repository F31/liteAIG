package adminapi

import (
	"testing"
	"time"
)

func TestLoginLimiterLocksOutRepeatedAccountFailures(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	limiter := NewLoginLimiter(time.Minute, 3, 100)
	for i := 0; i < 3; i++ {
		if limiter.AccountBlocked("1.2.3.4", "admin", base.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("failure %d should not yet be blocked", i)
		}
	}
	if !limiter.AccountBlocked("1.2.3.4", "admin", base.Add(3*time.Second)) {
		t.Fatal("fourth failure within window must be blocked")
	}
	// A different account from the same IP is independent.
	if limiter.AccountBlocked("1.2.3.4", "other", base.Add(3*time.Second)) {
		t.Fatal("different account must not inherit the lockout")
	}
	// The window slides: after it elapses the account is usable again.
	if limiter.AccountBlocked("1.2.3.4", "admin", base.Add(2*time.Minute)) {
		t.Fatal("account must unlock after the window elapses")
	}
}

func TestLoginLimiterBoundsPerIP(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	limiter := NewLoginLimiter(time.Minute, 100, 4)
	for i := 0; i < 4; i++ {
		if !limiter.IPAllowed("5.6.7.8", base.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("attempt %d should be allowed", i)
		}
	}
	if limiter.IPAllowed("5.6.7.8", base.Add(4*time.Second)) {
		t.Fatal("fifth attempt from the same IP within the window must be throttled")
	}
	if !limiter.IPAllowed("9.9.9.9", base.Add(4*time.Second)) {
		t.Fatal("a different IP must not be throttled")
	}
}

func TestClientIP(t *testing.T) {
	if got := clientIP("127.0.0.1:1234"); got != "127.0.0.1" {
		t.Fatalf("clientIP = %q", got)
	}
	if got := clientIP("[::1]:5678"); got != "::1" {
		t.Fatalf("clientIP v6 = %q", got)
	}
	if got := clientIP("no-port"); got != "no-port" {
		t.Fatalf("clientIP fallback = %q", got)
	}
}
