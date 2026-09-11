package adminapi

import (
	"net"
	"sync"
	"time"
)

// LoginLimiter is a small in-memory sliding-window rate limiter for the
// credential endpoint. It keeps two independent limits:
//
//   - per (client IP, username), counted on failures only: stops credential
//     guessing against one account without penalizing a correct login.
//   - per client IP, counted on every attempt: stops one client scanning many
//     usernames or flooding the endpoint.
//
// It is deliberately scoped to process memory: the Lite profile is
// single-node, so an in-memory window is the correct scope (a restart resets
// it, which is acceptable for this threat model).
type LoginLimiter struct {
	mu          sync.Mutex
	window      time.Duration
	maxFailures int // per (ip|username), failures only
	maxPerIP    int // per ip, all attempts
	hits        map[string][]time.Time
}

// NewLoginLimiter returns a limiter bounding failed attempts per account and
// total attempts per client within the sliding window.
func NewLoginLimiter(window time.Duration, maxFailures, maxPerIP int) *LoginLimiter {
	if window <= 0 {
		window = 15 * time.Minute
	}
	if maxFailures <= 0 {
		maxFailures = 5
	}
	if maxPerIP <= 0 {
		maxPerIP = 20
	}
	return &LoginLimiter{window: window, maxFailures: maxFailures, maxPerIP: maxPerIP, hits: map[string][]time.Time{}}
}

// Window exposes the configured sliding window (for Retry-After hints).
func (l *LoginLimiter) Window() time.Duration {
	return l.window
}

// allow records a hit for key and reports whether the per-key limit is
// already exhausted. When count is false the hit is not recorded (use for a
// read-only check).
func (l *LoginLimiter) allow(key string, t time.Time, limit int, record bool) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := t.Add(-l.window)
	recent := l.hits[key][:0]
	for _, hit := range l.hits[key] {
		if hit.After(cutoff) {
			recent = append(recent, hit)
		}
	}
	if len(recent) >= limit {
		if record {
			l.hits[key] = recent
		}
		return false
	}
	if record {
		l.hits[key] = append(recent, t)
	}
	return true
}

// IPAllowed reports whether the client IP may make another login attempt, and
// records the attempt.
func (l *LoginLimiter) IPAllowed(ip string, t time.Time) bool {
	return l.allow(ip, t, l.maxPerIP, true)
}

// AccountBlocked reports whether the (ip, username) pair has exhausted its
// failure budget, and records the failure. Call it only after a failed verify.
func (l *LoginLimiter) AccountBlocked(ip, username string, t time.Time) bool {
	return !l.allow(ip+"|"+username, t, l.maxFailures, true)
}

// clientIP extracts the peer IP from RemoteAddr for limiter keys.
func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
