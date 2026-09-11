package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/F31/liteAIG/internal/finops/budget"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/platform/coordination"
	"github.com/redis/go-redis/v9"
)

// coordinatorDeps carries the replaceable distributed-coordination primitives.
// Lite runs the self-contained in-memory implementations; Standard tier backs
// the same interfaces with Redis/Valkey via --coordinator.
type coordinatorDeps struct {
	leases   coordination.LeaseCoordinator
	inflight coordination.InflightCounter
	ledger   coordination.BudgetLedger // raw distributed ledger; fail mode is applied per policy (§14.4)
	client   *redis.Client             // non-nil only when Redis-backed; closed by close
	alerts   budget.AlertFunc          // degradation alerts (§14.4) wired to the caller's sink
}

// coordinatorOptions configures the coordinator assembly.
type coordinatorOptions struct {
	URL       string           // redis URL (redis://[user:pass@]host:port/db); empty = in-memory
	LedgerTTL time.Duration    // Redis reservation TTL; 0 uses the ledger default
	Alerts    budget.AlertFunc // degradation alerts wired by the caller
}

// close releases any external client held by the coordinator. It is safe to
// call on the in-memory assembly (no-op).
func (c *coordinatorDeps) close() {
	if c.client != nil {
		_ = c.client.Close()
	}
}

// openCoordinator builds the distributed-coordination primitives. With an empty
// URL it returns the self-contained in-memory implementations (Lite); with a
// redis:// URL it connects and returns the Redis variants (Standard tier). A
// redis URL with auth is honored through ParseURL; unreachable hosts and bad
// credentials fail fast at startup so misconfiguration is caught immediately.
func openCoordinator(ctx context.Context, opts coordinatorOptions, clock contracts.Clock) (*coordinatorDeps, error) {
	if strings.TrimSpace(opts.URL) == "" {
		return &coordinatorDeps{
			leases:   coordination.NewMemoryLeaseCoordinator(clock.Now),
			inflight: coordination.NewMemoryInflightCounter(),
			ledger:   coordination.NewMemoryBudgetLedger(clock.Now),
			alerts:   opts.Alerts,
		}, nil
	}
	options, err := redis.ParseURL(opts.URL)
	if err != nil {
		return nil, fmt.Errorf("parse --coordinator URL %q: %w", opts.URL, err)
	}
	client := redis.NewClient(options)
	// go-redis retries dials internally with backoff, so bound the eager
	// connectivity check with a context deadline rather than relying on a
	// DialTimeout alone (which the pool can exceed).
	pingCtx, cancelPing := context.WithTimeout(ctx, coordinatorDialTimeout)
	pingErr := client.Ping(pingCtx).Err()
	cancelPing()
	if pingErr != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect --coordinator %s: %w", hostOnly(opts.URL), pingErr)
	}
	return &coordinatorDeps{
		leases:   coordination.NewRedisLeaseCoordinator(client, clock.Now),
		inflight: coordination.NewRedisInflightCounter(client),
		ledger:   coordination.NewRedisBudgetLedger(client, clock.Now, opts.LedgerTTL),
		client:   client,
		alerts:   opts.Alerts,
	}, nil
}

// coordinatorDialTimeout bounds the eager startup connection so an unreachable
// coordinator fails fast instead of blocking process boot.
const coordinatorDialTimeout = 2 * time.Second

func hostOnly(url string) string {
	if index := strings.Index(url, "//"); index >= 0 {
		remainder := url[index+2:]
		if at := strings.Index(remainder, "@"); at >= 0 {
			remainder = remainder[at+1:]
		}
		if slash := strings.Index(remainder, "/"); slash >= 0 {
			return remainder[:slash]
		}
		return remainder
	}
	return url
}
