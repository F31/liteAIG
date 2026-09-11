package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/platform/storage/spool"
	"github.com/F31/liteAIG/internal/tenancy"
)

type recordingAccountingRepo struct {
	calls int
}

func (r *recordingAccountingRepo) Finalize(context.Context, accounting.Facts) (bool, error) {
	r.calls++
	return true, nil
}
func (r *recordingAccountingRepo) GetRequest(context.Context, tenancy.TenantScope, string) (*accounting.RequestRecord, error) {
	return nil, nil
}
func (r *recordingAccountingRepo) ListRequests(context.Context, tenancy.TenantScope, int) ([]accounting.RequestRecord, error) {
	return nil, nil
}
func (r *recordingAccountingRepo) GetUsage(context.Context, tenancy.TenantScope, string) (*accounting.UsageRecord, error) {
	return nil, nil
}

func TestSpoolAccountingRoutingAndSoftDegradation(t *testing.T) {
	registry := &runtime.ActiveRegistry{}
	registry.ActivateTenant("ref-a", runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant-a", TenantRef: "ref-a", Status: "active", Version: 1,
		Spool: runtime.SpoolPolicy{Mode: "soft", WarnThreshold: 0.7, CriticalThreshold: 0.9, HardThreshold: 1.0},
	}))

	direct := &recordingAccountingRepo{}
	spoolHandle, err := spool.New(spool.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	repo := &spoolAccounting{direct: direct, spool: spoolHandle, registry: registry}

	// A tenant with a spool policy appends to the spool, not the database.
	created, err := repo.Finalize(context.Background(), accounting.Facts{UsageEventID: "u1", RequestID: "r1", TenantID: "tenant-a", ProjectID: "p", CompletedAt: time.Now()})
	if err != nil || !created {
		t.Fatalf("Finalize() = %v, %v, want created", created, err)
	}
	if direct.calls != 0 {
		t.Fatalf("direct calls = %d, want 0 (spool-policy tenant)", direct.calls)
	}
	events, _, err := spoolHandle.TenantUsage(context.Background(), "tenant-a")
	if err != nil || events != 1 {
		t.Fatalf("tenant usage = %d, %v, want 1 pending event", events, err)
	}

	// A tenant without a snapshot writes straight through.
	if _, err := repo.Finalize(context.Background(), accounting.Facts{UsageEventID: "u2", RequestID: "r2", TenantID: "tenant-x"}); err != nil {
		t.Fatal(err)
	}
	if direct.calls != 1 {
		t.Fatalf("direct calls = %d, want 1 (unknown tenant)", direct.calls)
	}

	// Soft mode degrades on spool failure: the request still succeeds.
	if err := spoolHandle.Close(); err != nil {
		t.Fatal(err)
	}
	created, err = repo.Finalize(context.Background(), accounting.Facts{UsageEventID: "u3", RequestID: "r3", TenantID: "tenant-a"})
	if err != nil || !created {
		t.Fatalf("soft degradation = %v, %v, want success without error", created, err)
	}

	// Hard mode surfaces spool failure to the request.
	hardRegistry := &runtime.ActiveRegistry{}
	hardRegistry.ActivateTenant("ref-h", runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant-h", TenantRef: "ref-h", Status: "active", Version: 1,
		Spool: runtime.SpoolPolicy{Mode: "hard", WarnThreshold: 0.7, CriticalThreshold: 0.9, HardThreshold: 1.0},
	}))
	hardRepo := &spoolAccounting{direct: direct, spool: spoolHandle, registry: hardRegistry}
	if _, err := hardRepo.Finalize(context.Background(), accounting.Facts{UsageEventID: "u4", RequestID: "r4", TenantID: "tenant-h"}); !errors.Is(err, accounting.ErrSpoolUnavailable) {
		t.Fatalf("hard mode error = %v, want ErrSpoolUnavailable", err)
	}
}
