package adminapi

import (
	"sync"
	"time"
)

// AdminRateLimiter bounds authenticated control-plane traffic per client IP and
// per admin account. It is process-local by design: Lite/Standard already put
// gateway traffic behind dedicated limiters, while this protects the self-hosted
// management plane from accidental or low-effort floods.
type AdminRateLimiter struct {
	mu          sync.Mutex
	window      time.Duration
	maxPerIP    int
	maxPerAdmin int
	hits        map[string][]time.Time
}

func NewAdminRateLimiter(window time.Duration, maxPerIP, maxPerAdmin int) *AdminRateLimiter {
	if window <= 0 {
		window = time.Minute
	}
	if maxPerIP <= 0 {
		maxPerIP = 600
	}
	if maxPerAdmin <= 0 {
		maxPerAdmin = 120
	}
	return &AdminRateLimiter{window: window, maxPerIP: maxPerIP, maxPerAdmin: maxPerAdmin, hits: map[string][]time.Time{}}
}

func (l *AdminRateLimiter) Window() time.Duration { return l.window }

func (l *AdminRateLimiter) Allow(ip, adminID string, t time.Time) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ipKey := "ip:" + ip
	adminKey := "admin:" + adminID
	ipHits := l.recent(ipKey, t)
	adminHits := l.recent(adminKey, t)
	if len(ipHits) >= l.maxPerIP || len(adminHits) >= l.maxPerAdmin {
		l.hits[ipKey] = ipHits
		l.hits[adminKey] = adminHits
		return false
	}
	l.hits[ipKey] = append(ipHits, t)
	l.hits[adminKey] = append(adminHits, t)
	return true
}

func (l *AdminRateLimiter) recent(key string, t time.Time) []time.Time {
	cutoff := t.Add(-l.window)
	recent := l.hits[key][:0]
	for _, hit := range l.hits[key] {
		if hit.After(cutoff) {
			recent = append(recent, hit)
		}
	}
	return recent
}
