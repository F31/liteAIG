package adminapi

import (
	"sync"
	"time"
)

// ResetRateLimiter bounds how often password-reset codes can be requested.
// It keeps three independent limits so no single client or account can
// exhaust the mail relay or brute-force the flow:
//
//   - per client IP (all attempts): stops one host flooding many accounts.
//   - per (IP, username): stops one host hammering one account.
//   - per username, global minimum interval: stops the same account being
//     spammed from many IPs (the mail relay is the shared resource).
//
// Like LoginLimiter it is scoped to process memory, which is correct for the
// single-node Lite profile.
type ResetRateLimiter struct {
	mu            sync.Mutex
	window        time.Duration
	maxPerIP      int
	maxPerAccount int
	minInterval   time.Duration
	ipHits        map[string][]time.Time
	acctHits      map[string][]time.Time
	lastSent      map[string]time.Time
}

// NewResetRateLimiter returns a limiter with the given sliding window, per-IP
// and per-account caps, and a global per-username minimum gap.
func NewResetRateLimiter(window time.Duration, maxPerIP, maxPerAccount int, minInterval time.Duration) *ResetRateLimiter {
	if window <= 0 {
		window = 15 * time.Minute
	}
	if maxPerIP <= 0 {
		maxPerIP = 10
	}
	if maxPerAccount <= 0 {
		maxPerAccount = 5
	}
	if minInterval <= 0 {
		minInterval = time.Minute
	}
	return &ResetRateLimiter{
		window: window, maxPerIP: maxPerIP, maxPerAccount: maxPerAccount, minInterval: minInterval,
		ipHits: map[string][]time.Time{}, acctHits: map[string][]time.Time{}, lastSent: map[string]time.Time{},
	}
}

// Window exposes the sliding window (for Retry-After hints).
func (l *ResetRateLimiter) Window() time.Duration {
	return l.window
}

// AllowSend reports whether a code may be sent to username from ip at time t,
// recording the send when allowed.
func (l *ResetRateLimiter) AllowSend(ip, username string, t time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if last, ok := l.lastSent[username]; ok && t.Sub(last) < l.minInterval {
		return false
	}
	acctKey := ip + "|" + username
	ipRecent := l.recent(l.ipHits, ip, t)
	if len(ipRecent) >= l.maxPerIP {
		l.ipHits[ip] = ipRecent
		return false
	}
	acctRecent := l.recent(l.acctHits, acctKey, t)
	if len(acctRecent) >= l.maxPerAccount {
		l.acctHits[acctKey] = acctRecent
		return false
	}
	l.ipHits[ip] = append(ipRecent, t)
	l.acctHits[acctKey] = append(acctRecent, t)
	l.lastSent[username] = t
	return true
}

// recent returns the hits for key still inside the window, pruning the rest.
func (l *ResetRateLimiter) recent(store map[string][]time.Time, key string, t time.Time) []time.Time {
	cutoff := t.Add(-l.window)
	recent := store[key][:0]
	for _, hit := range store[key] {
		if hit.After(cutoff) {
			recent = append(recent, hit)
		}
	}
	return recent
}
