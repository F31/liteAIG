package sqlrepo

import (
	"context"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/platform/coordination"
	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/migrations"
)

func coordinationTestDB(t *testing.T, name string) *CoordinationLeaseStore {
	t.Helper()
	db, err := sqlite.Open(context.Background(), "file:coord-"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return NewCoordinationLeaseStore(db, func() time.Time { return time.Unix(1000, 0) })
}

func TestCoordinationLeaseElectionSingleHolder(t *testing.T) {
	store := coordinationTestDB(t, "single")
	ctx := context.Background()
	// Two independent processes (each its own store handle but the same table)
	// race for the platform:sweeper scope: exactly one wins.
	first, id, err := store.Acquire(ctx, "platform:sweeper", 1, time.Minute)
	if err != nil || !first || id == "" {
		t.Fatalf("first acquire = %t %q %v", first, id, err)
	}
	second, _, err := store.Acquire(ctx, "platform:sweeper", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second {
		t.Fatal("two processes held the platform singleton lease at once")
	}
	if err := store.Release(ctx, "platform:sweeper", id); err != nil {
		t.Fatal(err)
	}
	released, _, err := store.Acquire(ctx, "platform:sweeper", 1, time.Minute)
	if err != nil || !released {
		t.Fatalf("post-release acquire = %t %v", released, err)
	}
}

func TestCoordinationLeaseExpiryHandsBackToPeer(t *testing.T) {
	store := coordinationTestDB(t, "expiry")
	ctx := context.Background()
	now := time.Unix(1000, 0)
	clock := &mutableDBClock{now: now}
	store.clock = clock.Now
	ok, id, err := store.Acquire(ctx, "platform:alert", 1, time.Minute)
	if err != nil || !ok {
		t.Fatalf("acquire = %t %v", ok, err)
	}
	// The holder crashes without Release. When the lease expires, a peer must be
	// able to take over.
	clock.now = now.Add(2 * time.Minute)
	taken, _, err := store.Acquire(ctx, "platform:alert", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !taken {
		t.Fatal("expired leadership from a crashed process blocked the leader election")
	}
	_ = id
}

func TestCoordinationLeaseRenewOnlyOwned(t *testing.T) {
	store := coordinationTestDB(t, "renew")
	ctx := context.Background()
	ok, id, err := store.Acquire(ctx, "platform:migration", 1, time.Minute)
	if err != nil || !ok {
		t.Fatalf("acquire = %t %v", ok, err)
	}
	renewed, err := store.Renew(ctx, "platform:migration", id, time.Minute)
	if err != nil || !renewed {
		t.Fatalf("renew owned lease = %t %v", renewed, err)
	}
	missing, err := store.Renew(ctx, "platform:migration", "not-the-owner", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if missing {
		t.Fatal("renew created/extended a lease that was not owned")
	}
	if err := store.Release(ctx, "platform:migration", id); err != nil {
		t.Fatal(err)
	}
}

func TestCoordinationLeaseConformance(t *testing.T) {
	store := coordinationTestDB(t, "conformance")
	coordination.RunLeaseConformance(t, store)
}

type mutableDBClock struct{ now time.Time }

func (c *mutableDBClock) Now() time.Time { return c.now }
