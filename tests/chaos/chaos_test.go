package chaos

import (
	"context"
	"database/sql"
	"errors"
	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/finops/budget"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/platform/clock"
	"github.com/F31/liteAIG/internal/platform/coordination"
	postgresstore "github.com/F31/liteAIG/internal/platform/storage/postgres"
	"github.com/F31/liteAIG/internal/platform/storage/spool"
	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/internal/tenancy"
	"github.com/F31/liteAIG/migrations"
	"github.com/redis/go-redis/v9"
	"os"
	"testing"
	"time"
)

func TestRedisOutageHonorsHardAndSoftBudgetModes(t *testing.T) {
	addr := os.Getenv("LITEAIG_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("redis not configured")
	}
	client := redis.NewClient(&redis.Options{Addr: addr, MaxRetries: 0, DialTimeout: 50 * time.Millisecond, ReadTimeout: 50 * time.Millisecond, WriteTimeout: 50 * time.Millisecond})
	ledger := coordination.NewRedisBudgetLedger(client, nil, time.Minute)
	_ = client.Close()
	request := coordination.ReserveRequest{TenantID: "chaos", ReservationID: "r", Estimate: 1, Windows: []coordination.BudgetWindow{{Key: "budget:{tenant:chaos}:tenant:1d", Limit: 10}}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	hard := budget.NewFailModeLedger(ledger, budget.FailClosed, nil)
	if _, err := hard.Reserve(ctx, request); !errors.Is(err, coordination.ErrLedgerUnavailable) {
		t.Fatalf("hard mode error=%v", err)
	}
	soft := budget.NewFailModeLedger(ledger, budget.FailOpen, nil)
	result, err := soft.Reserve(ctx, request)
	if err != nil || !result.Reserved || !result.Degraded {
		t.Fatalf("soft mode=%+v,%v", result, err)
	}
}

func TestDataPlaneSnapshotSurvivesPostgresOutage(t *testing.T) {
	dsn := os.Getenv("LITEAIG_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("postgres not configured")
	}
	db, err := postgresstore.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	registry := &runtime.ActiveRegistry{}
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "tenant", TenantRef: "ref", Version: 7, Status: "active"})
	registry.ActivateTenant("ref", snapshot)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	loaded, ok := registry.Tenant("ref")
	if !ok || loaded.Version != 7 {
		t.Fatalf("snapshot unavailable after Postgres outage: %+v", loaded)
	}
}

const (
	spoolChaosTenantID  = "60000000-0000-4000-8000-000000000010"
	spoolChaosProjectID = "60000000-0000-4000-8000-000000000011"
)

// downIngest simulates an Accounting store outage that fails every ingest.
type downIngest struct{}

func (downIngest) Finalize(context.Context, accounting.Facts) (bool, error) {
	return false, errors.New("accounting db down")
}
func (downIngest) GetRequest(context.Context, tenancy.TenantScope, string) (*accounting.RequestRecord, error) {
	return nil, tenancy.ErrNotFound
}
func (downIngest) ListRequests(context.Context, tenancy.TenantScope, int) ([]accounting.RequestRecord, error) {
	return nil, tenancy.ErrNotFound
}
func (downIngest) GetUsage(context.Context, tenancy.TenantScope, string) (*accounting.UsageRecord, error) {
	return nil, tenancy.ErrNotFound
}

func TestAccountingSpoolSurvivesStoreOutageAndReplays(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, "file:chaos-spool?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, 'chaos-spool-ref', 'Chaos Spool')`, spoolChaosTenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id, tenant_id, name) VALUES ($1, $2, 'Default')`, spoolChaosProjectID, spoolChaosTenantID); err != nil {
		t.Fatal(err)
	}
	ingest := sqlrepo.NewAccountingRepository(db)

	s, err := spool.New(spool.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	policy := accounting.SpoolPolicy{Mode: "soft", WarnThreshold: 0.7, CriticalThreshold: 0.9, HardThreshold: 1.0}
	repo := accounting.NewSpoolRepository(s, policy)

	// Provider-produced usage finalizes while the Accounting store is down:
	// events must persist in the local WAL.
	for i := 0; i < 3; i++ {
		facts := accounting.Facts{
			UsageEventID: "60000000-0000-4000-8000-00000000001" + string(rune('a'+i)),
			RequestID:    "60000000-0000-4000-8000-00000000002" + string(rune('a'+i)),
			TenantID:     spoolChaosTenantID, ProjectID: spoolChaosProjectID,
			LogicalModel: "chat", DeploymentID: "deployment", Outcome: "success",
			UsageSource: "api", InputTokens: 3, OutputTokens: 2,
			SnapshotVersion: 1, SecurityEpoch: 1,
			ReceivedAt: time.Unix(1, 0), CompletedAt: time.Unix(2, 0),
		}
		if _, err := repo.Finalize(ctx, facts); err != nil {
			t.Fatalf("finalize during outage=%v", err)
		}
	}

	// Flushing against the down store must not lose or commit events.
	down := accounting.NewFlusher(s, downIngest{}, accounting.FlusherConfig{Batch: 100, Interval: time.Second}, nil, clock.System{}, nil)
	if ingested, err := down.RunOnce(ctx); err == nil && ingested != 0 {
		t.Fatalf("flush during outage ingested=%d, want 0", ingested)
	}
	pending, err := s.Pending(ctx, 100)
	if err != nil || len(pending) != 3 {
		t.Fatalf("pending after outage=%d,%v, want 3", len(pending), err)
	}

	// After recovery, replay must ingest exactly once with zero duplicates.
	recovered := accounting.NewFlusher(s, ingest, accounting.FlusherConfig{Batch: 100, Interval: time.Second}, nil, clock.System{}, nil)
	if ingested, err := recovered.RunOnce(ctx); err != nil || ingested != 3 {
		t.Fatalf("recovered RunOnce()=%d,%v", ingested, err)
	}
	if ingested, err := recovered.RunOnce(ctx); err != nil || ingested != 0 {
		t.Fatalf("replay RunOnce()=%d,%v", ingested, err)
	}
	assertRowCounts(t, db)
}

func assertRowCounts(t *testing.T, db *sql.DB) {
	t.Helper()
	var requests, usage int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_records WHERE tenant_id=$1`, spoolChaosTenantID).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_events WHERE tenant_id=$1`, spoolChaosTenantID).Scan(&usage); err != nil {
		t.Fatal(err)
	}
	if requests != 3 || usage != 3 {
		t.Fatalf("replayed rows request=%d usage=%d, want 3/3 (no duplicates)", requests, usage)
	}
}
