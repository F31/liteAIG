package cache

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func testKey(tenant, project string) Key {
	return Key{TenantID: tenant, ProjectID: project, LogicalModel: "chat", NormalizedRequest: `{"kind":"chat"}`, NamespaceVersion: 1}
}

func TestKeyScopingSeparatesTenantsAndProjects(t *testing.T) {
	a := testKey("t1", "p1")
	b := testKey("t2", "p1")
	c := testKey("t1", "p2")
	d := testKey("t1", "p1")
	if a.String() == b.String() || a.String() == c.String() {
		t.Fatal("keys not scoped by tenant/project")
	}
	if a.String() != d.String() {
		t.Fatal("identical scope produced different keys")
	}
	d.NamespaceVersion = 2
	if a.String() == d.String() {
		t.Fatal("namespace version did not invalidate the key")
	}
}

func TestMemoryStoreGetPutDeleteAndEviction(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore(MemoryConfig{Capacity: 2})
	entry := &Entry{Response: json.RawMessage(`{"model":"m"}`), StoredAt: time.Now(), TTL: time.Hour}

	if _, err := store.Get(ctx, testKey("t", "p").String()); err != ErrMiss {
		t.Fatalf("Get() empty = %v", err)
	}
	if err := store.Put(ctx, testKey("t", "p").String(), entry); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, testKey("t", "p").String())
	if err != nil || got == nil {
		t.Fatalf("Get() after Put = %+v, %v", got, err)
	}

	// Capacity 2: adding two more evicts the first.
	if err := store.Put(ctx, testKey("t", "p2").String(), entry); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, testKey("t", "p3").String(), entry); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, testKey("t", "p").String()); err != ErrMiss {
		t.Fatalf("expected eviction, got %v", err)
	}

	if err := store.Delete(ctx, testKey("t", "p2").String()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, testKey("t", "p2").String()); err != ErrMiss {
		t.Fatalf("delete did not remove entry: %v", err)
	}
}

func TestMemoryStoreTTLExpiry(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore(MemoryConfig{Capacity: 8})
	now := time.Unix(1000, 0)
	store.clock = func() time.Time { return now }
	if err := store.Put(ctx, testKey("t", "p").String(), &Entry{Response: json.RawMessage(`{}`), StoredAt: now, TTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := store.Get(ctx, testKey("t", "p").String()); err != ErrMiss {
		t.Fatalf("expired entry served: %v", err)
	}
}
