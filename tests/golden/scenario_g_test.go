package golden

import (
	"context"
	"github.com/F31/liteAIG/internal/app"
	"github.com/F31/liteAIG/internal/finops/accounting"
	platformclock "github.com/F31/liteAIG/internal/platform/clock"
	platformsecrets "github.com/F31/liteAIG/internal/platform/secrets"
	"github.com/F31/liteAIG/internal/platform/storage/spool"
	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/migrations"
	"sync/atomic"
	"testing"
	"time"
)

const (
	scenarioGTenantID  = "60000000-0000-4000-8000-000000000020"
	scenarioGProjectID = "60000000-0000-4000-8000-000000000021"
)

func TestScenarioGAccountingSpoolReplayEquivalence(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, "file:scenario-g-spool?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tenants(id, public_ref, name) VALUES ($1, 'sg-ref', 'Scenario G')`, scenarioGTenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id, tenant_id, name) VALUES ($1, $2, 'Default')`, scenarioGProjectID, scenarioGTenantID); err != nil {
		t.Fatal(err)
	}
	ingest := sqlrepo.NewAccountingRepository(db)
	s, err := spool.New(spool.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	policy := accounting.SpoolPolicy{Mode: "hard", WarnThreshold: 0.7, CriticalThreshold: 0.9, HardThreshold: 1.0}
	repo := accounting.NewSpoolRepository(s, policy)
	for i := 0; i < 4; i++ {
		facts := accounting.Facts{
			UsageEventID: "60000000-0000-4000-8000-00000000003" + string(rune('a'+i)),
			RequestID:    "60000000-0000-4000-8000-00000000004" + string(rune('a'+i)),
			TenantID:     scenarioGTenantID, ProjectID: scenarioGProjectID,
			LogicalModel: "chat", Outcome: "success", InputTokens: 1, OutputTokens: 1,
			SnapshotVersion: 1, SecurityEpoch: 1, ReceivedAt: time.Unix(1, 0), CompletedAt: time.Unix(2, 0),
		}
		if _, err := repo.Finalize(ctx, facts); err != nil {
			t.Fatalf("finalize=%v", err)
		}
	}
	flusher := accounting.NewFlusher(s, ingest, accounting.FlusherConfig{Batch: 100, Interval: time.Second}, nil, platformclock.System{}, nil)
	if ingested, err := flusher.RunOnce(ctx); err != nil || ingested != 4 {
		t.Fatalf("RunOnce()=%d,%v", ingested, err)
	}
	if ingested, err := flusher.RunOnce(ctx); err != nil || ingested != 0 {
		t.Fatalf("replay RunOnce()=%d,%v", ingested, err)
	}
	var requests, usage int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_records WHERE tenant_id=$1`, scenarioGTenantID).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_events WHERE tenant_id=$1`, scenarioGTenantID).Scan(&usage); err != nil {
		t.Fatal(err)
	}
	if requests != 4 || usage != 4 {
		t.Fatalf("replay rows request=%d usage=%d, want 4/4", requests, usage)
	}
}

func TestScenarioGGracefulDrainSmoke(t *testing.T) {
	lifecycle, err := app.NewLifecycle(app.DrainConfig{DrainTimeout: 500 * time.Millisecond, StreamDrainTimeout: 500 * time.Millisecond, ForceShutdownTimeout: time.Second}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !lifecycle.Ready() {
		t.Fatal("expected ready before drain")
	}
	var finalized atomic.Int32
	lifecycle.AddInflight()
	go func() {
		time.Sleep(30 * time.Millisecond)
		finalized.Add(1) // budget/lease finalization
		lifecycle.DoneInflight()
	}()
	if err := lifecycle.BeginDrain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lifecycle.Ready() {
		t.Fatal("readyz must be not-ready after drain")
	}
	if finalized.Load() != 1 {
		t.Fatal("in-flight finalization did not complete before exit")
	}
}

func TestScenarioGSecretOutageFailOpenFailClosed(t *testing.T) {
	ctx := context.Background()
	provider := platformsecrets.NewMemoryProvider(map[string][]byte{"secret://sg/cred": []byte("material")})
	cached, err := platformsecrets.NewCachingProvider(provider, platformsecrets.CacheConfig{TTL: time.Minute, StaleGrace: 30 * time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	cached.SetNowForTest(func() time.Time { return now })
	if _, err := cached.Resolve(ctx, "secret://sg/cred"); err != nil {
		t.Fatal(err)
	}
	// Secret Provider outage within grace → cached credential continues.
	provider.RevokeAll()
	now = now.Add(70 * time.Second)
	value, err := cached.Resolve(ctx, "secret://sg/cred")
	if err != nil || string(value) != "material" || !cached.Degraded("secret://sg/cred") {
		t.Fatalf("fail-open within grace = %q,%v", value, err)
	}
	// After grace → fail closed.
	now = now.Add(time.Minute)
	if _, err := cached.Resolve(ctx, "secret://sg/cred"); err == nil {
		t.Fatal("expected fail-closed after grace")
	}
}
