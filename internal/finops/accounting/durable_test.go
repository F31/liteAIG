package accounting_test

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/platform/clock"
	"github.com/F31/liteAIG/internal/platform/storage/spool"
	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/migrations"
)

const (
	spoolTenantID  = "60000000-0000-4000-8000-000000000001"
	spoolProjectID = "60000000-0000-4000-8000-000000000002"
)

func newSpoolFacts(id int64) accounting.Facts {
	return accounting.Facts{
		UsageEventID:    "60000000-0000-4000-8000-0000000000" + string(rune('a'+id)),
		RequestID:       "60000000-0000-4000-8000-0000000000" + string(rune('b'+id)),
		TenantID:        spoolTenantID,
		ProjectID:       spoolProjectID,
		LogicalModel:    "chat",
		DeploymentID:    "deployment",
		Outcome:         "success",
		UsageSource:     "api",
		InputTokens:     3,
		OutputTokens:    2,
		SnapshotVersion: 1,
		SecurityEpoch:   1,
		ReceivedAt:      time.Unix(1, 0),
		CompletedAt:     time.Unix(2, 0),
	}
}

func newSQLiteAccounting(t *testing.T) (*sql.DB, accounting.Repository) {
	t.Helper()
	db, err := sqlite.Open(context.Background(), "file:spool-replay?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, 'spool-ref', 'Spool')`, spoolTenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id, tenant_id, name) VALUES ($1, $2, 'Default')`, spoolProjectID, spoolTenantID); err != nil {
		t.Fatal(err)
	}
	return db, sqlrepo.NewAccountingRepository(db)
}

func TestFlusherReplaysIdempotentlyNoDuplicates(t *testing.T) {
	ctx := context.Background()
	db, ingest := newSQLiteAccounting(t)
	s, err := spool.New(spool.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	policy := accounting.SpoolPolicy{Mode: "soft", WarnThreshold: 0.7, CriticalThreshold: 0.9, HardThreshold: 1.0}
	repo := accounting.NewSpoolRepository(s, policy)
	for i := 0; i < 3; i++ {
		if _, err := repo.Finalize(ctx, newSpoolFacts(int64(i))); err != nil {
			t.Fatalf("Finalize()=%v", err)
		}
	}
	flusher := accounting.NewFlusher(s, ingest, accounting.FlusherConfig{Batch: 100, Interval: time.Second}, nil, clock.System{}, nil)
	ingested, err := flusher.RunOnce(ctx)
	if err != nil || ingested != 3 {
		t.Fatalf("RunOnce()=%d,%v", ingested, err)
	}
	// Replay must not create duplicates.
	again, err := flusher.RunOnce(ctx)
	if err != nil || again != 0 {
		t.Fatalf("replay RunOnce()=%d,%v", again, err)
	}
	var requests, usage int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_records WHERE tenant_id=$1`, spoolTenantID).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_events WHERE tenant_id=$1`, spoolTenantID).Scan(&usage); err != nil {
		t.Fatal(err)
	}
	if requests != 3 || usage != 3 {
		t.Fatalf("rows request=%d usage=%d, want 3/3", requests, usage)
	}
	if got := s.SegmentCount(); got != 0 {
		t.Fatalf("segments after full commit = %d, want 0", got)
	}
}

func TestDurableFinalizerBudgetIndependentOfIngest(t *testing.T) {
	ctx := context.Background()
	s, err := spool.New(spool.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	policy := accounting.SpoolPolicy{Mode: "soft"}
	repo := accounting.NewSpoolRepository(s, policy)
	budget := &countingBudget{}
	facts := newSpoolFacts(7)
	facts.BudgetPolicyID = "budget"
	facts.ReservationID = "reservation"
	actual := int64(4)
	facts.ActualTokens = &actual
	finalizer, err := accounting.NewFinalizer(accounting.FinalizerConfig{Timeout: time.Second}, repo, budget, nil, facts)
	if err != nil {
		t.Fatal(err)
	}
	durable := accounting.NewDurableFinalizer(finalizer, s, policy, nil, nil, clock.System{})
	if _, err := durable.Finalize(ctx, facts); err != nil {
		t.Fatalf("Finalize()=%v", err)
	}
	// Budget reconcile ran even though the ingest repository has not drained.
	if budget.reconciles.Load() != 1 {
		t.Fatalf("budget reconciles = %d", budget.reconciles.Load())
	}
	pending, err := s.Pending(ctx, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %d,%v", len(pending), err)
	}
}

func TestSoftModeContinuesOnSpoolUnavailable(t *testing.T) {
	ctx := context.Background()
	failing := &failingSpool{}
	repo := accounting.NewSpoolRepository(failing, accounting.SpoolPolicy{Mode: "soft"})
	facts := newSpoolFacts(9)
	finalizer, err := accounting.NewFinalizer(accounting.FinalizerConfig{Timeout: time.Second}, repo, &countingBudget{}, nil, facts)
	if err != nil {
		t.Fatal(err)
	}
	durable := accounting.NewDurableFinalizer(finalizer, failing, accounting.SpoolPolicy{Mode: "soft"}, nil, &recordingSink{}, clock.System{})
	if _, err := durable.Finalize(ctx, facts); err != nil {
		t.Fatalf("soft mode must continue on spool outage, got %v", err)
	}
}

func TestHardModeRejectsOnSpoolFull(t *testing.T) {
	ctx := context.Background()
	s, err := spool.New(spool.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Pre-populate pressure so the byte-based hard gate trips.
	if err := s.Append(ctx, accounting.SpoolRecord{EventID: "pre", RequestID: "pre-req", TenantID: spoolTenantID, ProjectID: spoolProjectID, EventTS: time.Now(), Facts: newSpoolFacts(1)}); err != nil {
		t.Fatal(err)
	}
	policy := accounting.SpoolPolicy{Mode: "hard", HardThreshold: 1.0, QuotaBytes: 40}
	repo := accounting.NewSpoolRepository(s, policy)
	facts := newSpoolFacts(2)
	finalizer, err := accounting.NewFinalizer(accounting.FinalizerConfig{Timeout: time.Second}, repo, &countingBudget{}, nil, facts)
	if err != nil {
		t.Fatal(err)
	}
	durable := accounting.NewDurableFinalizer(finalizer, s, policy, nil, nil, clock.System{})
	if _, err := durable.Finalize(ctx, facts); !errors.Is(err, accounting.ErrSpoolFull) {
		t.Fatalf("hard mode must reject when spool is full, got %v", err)
	}
}

type countingBudget struct {
	reconciles atomic.Int32
}

func (b *countingBudget) Reconcile(string, string, int64) error { b.reconciles.Add(1); return nil }
func (b *countingBudget) Release(string, string) error          { return nil }

type failingSpool struct{}

func (f *failingSpool) Append(context.Context, accounting.SpoolRecord) error {
	return accounting.ErrSpoolUnavailable
}
func (f *failingSpool) Pending(context.Context, int) ([]accounting.SpoolRecord, error) {
	return nil, nil
}
func (f *failingSpool) Commit(context.Context, []string) error { return nil }
func (f *failingSpool) Stats(context.Context) (accounting.SpoolStats, error) {
	return accounting.SpoolStats{}, nil
}
func (f *failingSpool) TenantUsage(context.Context, string) (int64, int64, error) {
	return 0, 0, nil
}

type recordingSink struct {
	events int
}

func (r *recordingSink) Emit(_ context.Context, _ contracts.DomainEvent) error {
	r.events++
	return nil
}
