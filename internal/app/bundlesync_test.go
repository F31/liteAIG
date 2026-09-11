package app

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/platform/bundle"
)

// mountBundleHandler wires the control-plane bundle handler onto a mux so the
// {tenantRef} path value is populated (required for the transport to resolve).
func mountBundleHandler(handler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /api/runtime/bundles/{tenantRef}", handler)
	mux.Handle("GET /api/runtime/bundles", handler)
	return mux
}

// TestBundleSyncerRollingUpgrade is the HA-gate rolling-upgrade subset: a
// gateway that already activated bundle v1 re-syncs and atomically activates
// the newer signed bundle v2 published by the Control Plane.
func TestBundleSyncerRollingUpgrade(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer := bundle.NewSigner(privateKey, "v1", 2)
	token := "token"
	hub := bundle.NewHub()
	server := httptest.NewServer(mountBundleHandler(bundle.NewHandler(hub, token)))
	defer server.Close()

	registry := &runtime.ActiveRegistry{}
	client := bundle.NewClient(server.URL, token, nil)
	syncer := newBundleSyncer(client, signer.PublicKey(), registry, systemClock{}, time.Hour)

	publish := func(version int64) {
		snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
			TenantID: "tenant", TenantRef: "ref", Status: "active", Version: version,
			PublishedAt: time.Unix(10+version, 0),
			Credentials: []runtime.Credential{{ID: "c", ProviderID: "p", SecretRef: "secret://tenant/c", Status: "enabled"}},
		})
		signed, err := signer.Sign(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		hub.Publish(snapshot, signed)
	}

	publish(1)
	if err := syncer.syncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	activated, ok := registry.Tenant("ref")
	if !ok || activated.Version != 1 {
		version := int64(-1)
		if ok {
			version = activated.Version
		}
		t.Fatalf("after v1 sync: ok=%t version=%d", ok, version)
	}

	// The Control Plane publishes v2; the gateway picks it up on the next sync.
	publish(2)
	if err := syncer.syncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	upgraded, ok := registry.Tenant("ref")
	if !ok || upgraded.Version != 2 {
		version := int64(-1)
		if ok {
			version = upgraded.Version
		}
		t.Fatalf("after v2 sync: ok=%t version=%d", ok, version)
	}
}

// TestBundleSyncerFaultInjection verifies a pull failure on an unreachable
// Control Plane leaves the previously activated runtime untouched (fail-open,
// spec §2.8.2 "Control Plane Failure Isolation"). It also verifies the syncer
// is a no-op when no public key is configured.
func TestBundleSyncerFaultInjection(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(nil)
	signer := bundle.NewSigner(privateKey, "v1", 2)
	token := "token"
	hub := bundle.NewHub()
	server := httptest.NewServer(mountBundleHandler(bundle.NewHandler(hub, token)))

	registry := &runtime.ActiveRegistry{}
	client := bundle.NewClient(server.URL, token, nil)
	syncer := newBundleSyncer(client, signer.PublicKey(), registry, systemClock{}, time.Hour)

	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Status: "active", Version: 1,
		PublishedAt: time.Unix(11, 0),
	})
	signed, _ := signer.Sign(snapshot)
	hub.Publish(snapshot, signed)
	if err := syncer.syncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Kill the Control Plane.
	server.Close()
	if err := syncer.syncOnce(context.Background()); err == nil {
		t.Fatal("sync on a dead Control Plane must surface the transport error")
	}
	if activated, ok := registry.Tenant("ref"); !ok || activated.Version != 1 {
		version := int64(-1)
		if ok {
			version = activated.Version
		}
		t.Fatalf("runtime lost after control-plane failure: ok=%t version=%d", ok, version)
	}

	// A syncer without a public key is a no-op (monolithic mode).
	noop := newBundleSyncer(client, nil, registry, systemClock{}, time.Hour)
	if err := noop.syncOnce(context.Background()); err != nil {
		t.Fatalf("no-key syncer must not fail: %v", err)
	}
}
