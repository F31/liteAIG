package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/tenancy"
)

// spoolAccounting routes accounting finalization per the tenant's compiled
// spool policy: tenants without a spool policy write straight to the
// authoritative store; hard/soft tenants append to the durable spool and a
// background flusher ingests into the authoritative store (at-least-once,
// idempotent by request id). In soft mode spool pressure never fails the
// request; the accounting event degrades instead.
type spoolAccounting struct {
	direct   accounting.Repository
	spool    accounting.Spool
	registry *runtime.ActiveRegistry
}

func (r *spoolAccounting) Finalize(ctx context.Context, facts accounting.Facts) (bool, error) {
	snapshot, ok := r.registry.TenantByID(facts.TenantID)
	if !ok {
		return r.direct.Finalize(ctx, facts)
	}
	policy := snapshot.SpoolPolicy()
	if policy.Mode != "hard" && policy.Mode != "soft" {
		return r.direct.Finalize(ctx, facts)
	}
	spoolPolicy := accounting.SpoolPolicy{
		Mode:              policy.Mode,
		WarnThreshold:     policy.WarnThreshold,
		CriticalThreshold: policy.CriticalThreshold,
		HardThreshold:     policy.HardThreshold,
		QuotaBytes:        policy.QuotaBytes,
	}
	repo := accounting.NewSpoolRepository(r.spool, spoolPolicy)
	created, err := repo.Finalize(ctx, facts)
	if err != nil && policy.Mode == "soft" && (errors.Is(err, accounting.ErrSpoolUnavailable) || errors.Is(err, accounting.ErrSpoolFull)) {
		return true, nil
	}
	return created, err
}

func (r *spoolAccounting) GetRequest(ctx context.Context, scope tenancy.TenantScope, id string) (*accounting.RequestRecord, error) {
	return r.direct.GetRequest(ctx, scope, id)
}

func (r *spoolAccounting) ListRequests(ctx context.Context, scope tenancy.TenantScope, limit int) ([]accounting.RequestRecord, error) {
	return r.direct.ListRequests(ctx, scope, limit)
}

func (r *spoolAccounting) GetUsage(ctx context.Context, scope tenancy.TenantScope, requestID string) (*accounting.UsageRecord, error) {
	return r.direct.GetUsage(ctx, scope, requestID)
}

var _ accounting.Repository = (*spoolAccounting)(nil)

// liteDataPath resolves the on-disk database path from a file: SQLite DSN.
// In-memory databases have no durable spool location and report ok=false.
func liteDataPath(dsn string) (string, bool) {
	path := strings.TrimSpace(dsn)
	if !strings.HasPrefix(path, "file:") {
		return "", false
	}
	path = strings.TrimPrefix(path, "file:")
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if path == "" || path == ":memory:" || strings.Contains(dsn, "mode=memory") {
		return "", false
	}
	return path, true
}

// spoolDrainTimeout bounds the final spool flush on shutdown.
const spoolDrainTimeout = 10 * time.Second
