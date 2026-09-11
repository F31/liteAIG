package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/migrations"
)

// singletonTestDB builds an in-memory DB with migrations applied, backed by a
// deterministic clock.
func singletonTestDB(t *testing.T, name string) *sqlrepo.CoordinationLeaseStore {
	t.Helper()
	db, err := sqlite.Open(context.Background(), "file:singleton-"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return sqlrepo.NewCoordinationLeaseStore(db, time.Now)
}

// The sweeper singleton runs its job only while it holds the platform lease;
// when a peer process wins the lease first, the sweeper stays idle until the
// leader releases, then takes over.
func TestSingletonSweeperLeaderOnlyRunsJob(t *testing.T) {
	store := singletonTestDB(t, "leader")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sweeps int32
	// Process A claims the platform:sweeper lease up front, so B must not run.
	ok, leaseID, err := store.Acquire(ctx, platformSweeperScope, 1, singletonLeaseTTL)
	if err != nil || !ok {
		t.Fatalf("pre-acquire = %t %v", ok, err)
	}

	sweeper := &singletonTask{leases: store, scope: platformSweeperScope, ttl: singletonLeaseTTL}
	done := make(chan struct{})
	go func() {
		sweeper.run(ctx, 20*time.Millisecond, func(context.Context) error {
			atomic.AddInt32(&sweeps, 1)
			return nil
		})
		close(done)
	}()

	time.Sleep(80 * time.Millisecond)
	if sweeps := atomic.LoadInt32(&sweeps); sweeps != 0 {
		t.Fatalf("non-leader ran the sweeper %d times", sweeps)
	}
	if err := store.Release(ctx, platformSweeperScope, leaseID); err != nil {
		t.Fatal(err)
	}
	// On the next interval the sweeper acquires leadership and starts running.
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&sweeps) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if success := atomic.LoadInt32(&sweeps) > 0; !success {
		t.Fatal("leader never ran the sweeper after acquiring the lease")
	}
	cancel()
	<-done
}

// The singleton task releases its lease on shutdown so the next process can
// become leader immediately instead of waiting for the TTL to expire.
func TestSingletonReleasesLeaseOnShutdown(t *testing.T) {
	store := singletonTestDB(t, "shutdown")
	ctx, cancel := context.WithCancel(context.Background())
	sweeper := &singletonTask{leases: store, scope: "platform:test", ttl: singletonLeaseTTL}
	done := make(chan struct{})
	go func() {
		sweeper.run(ctx, 10*time.Millisecond, func(context.Context) error { return nil })
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	ok, _, err := store.Acquire(context.Background(), "platform:test", 1, singletonLeaseTTL)
	if err != nil || !ok {
		t.Fatalf("lease not released on shutdown: acquire = %t %v", ok, err)
	}
}
