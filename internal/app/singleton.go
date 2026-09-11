package app

import (
	"context"
	"log"
	"time"

	"github.com/F31/liteAIG/internal/platform/coordination"
)

// singletonTask runs a Control-Plane job on exactly one process at a time. The
// DB-backed coordination_leases store provides the leader-election primitive
// (§2.2): each scope (e.g. platform:sweeper) has one row, so only the process
// that holds the lease performs the work while the others wait. A crashed
// leader loses the lease at expiry and another process takes over.
type singletonTask struct {
	leases coordination.LeaseCoordinator
	scope  string
	ttl    time.Duration
}

// singletonLeaseTTL bounds how long a process may hold a singleton lease before
// it must renew. The run loop renews each interval, so a holder that crashes
// (or a hiccup longer than the TTL) yields leadership.
const singletonLeaseTTL = 30 * time.Second

// platformSweeperScope is the coordination_leases scope for the platform-wide
// Reservation Sweeper singleton (§14.3).
const platformSweeperScope = "platform:sweeper"

// platformA2APushScope coordinates the durable A2A push callback outbox worker
// so Standard-tier multi-process deployments do not drain the same row twice.
const platformA2APushScope = "platform:a2a_push_outbox"

// platformEventOutboxScope coordinates the durable domain-event outbox
// delivery worker across replicas.
const platformEventOutboxScope = "platform:event_outbox"

// reservationSweepInterval is the sweeper cadence the spec fixes for the
// Control Plane job (default 10s).
const reservationSweepInterval = 10 * time.Second

// run acquires the scope lease at each interval. While it owns the lease it
// invokes job; when it loses the lease (a peer took over or the lease expired)
// it stops running the job until it can re-acquire, keeping the singleton
// task always executed by exactly one process.
func (s *singletonTask) run(ctx context.Context, interval time.Duration, job func(context.Context) error) {
	if s == nil || s.leases == nil || s.scope == "" || job == nil {
		return
	}
	leaseID := ""
	holder := false
	for {
		if !holder {
			if ok, id, err := s.leases.Acquire(ctx, s.scope, 1, s.ttl); err != nil {
				// Fail-open: an unresponsive lease table must not crash the
				// process. Log and retry on the next interval.
				log.Printf("singleton %s: lease acquire failed: %v", s.scope, err)
			} else if ok {
				holder = true
				leaseID = id
				log.Printf("singleton %s: acquired platform lease", s.scope)
				if err := job(ctx); err != nil {
					log.Printf("singleton %s: job failed: %v", s.scope, err)
				}
			}
		} else {
			// We are the leader. Renew before the next job so a stale lease
			// never runs the job twice concurrently across processes.
			if renewed, err := s.leases.Renew(ctx, s.scope, leaseID, s.ttl); err != nil {
				log.Printf("singleton %s: lease renew failed: %v", s.scope, err)
			} else if !renewed {
				holder = false
				leaseID = ""
				log.Printf("singleton %s: lost platform lease", s.scope)
			} else {
				if err := job(ctx); err != nil {
					log.Printf("singleton %s: job failed: %v", s.scope, err)
				}
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			if holder {
				_ = s.leases.Release(context.Background(), s.scope, leaseID)
			}
			return
		case <-timer.C:
		}
	}
}
