package bundle

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

const testToken = "shared-bundle-token"

func transportSnapshot(version int64) *runtime.TenantRuntimeSnapshot {
	return runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Status: "active", Version: version, SecurityEpoch: 3,
		PublishedAt: time.Unix(10, 0),
		Credentials: []runtime.Credential{{ID: "c", ProviderID: "p", SecretRef: "secret://tenant/c", Status: "enabled"}},
	})
}

func signed(t *testing.T, snapshot *runtime.TenantRuntimeSnapshot) (*Signer, *RuntimeBundle) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer := NewSigner(privateKey, testSchema, 2)
	bundle, err := signer.Sign(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return signer, bundle
}

func mount(handler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /api/runtime/bundles/{tenantRef}", handler)
	mux.Handle("GET /api/runtime/bundles", handler)
	return mux
}

func TestHubAndHTTPRoundTrip(t *testing.T) {
	signer, bundle := signed(t, transportSnapshot(4))
	hub := NewHub()
	hub.Publish(bundle.Snapshot, bundle)

	// Unauthorized is rejected.
	mux := mount(NewHandler(hub, testToken))
	req := httptest.NewRequest(http.MethodGet, "/api/runtime/bundles/ref", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", rec.Code)
	}

	// Authorized fetch round-trips the signed bundle.
	req = httptest.NewRequest(http.MethodGet, "/api/runtime/bundles/ref", nil)
	req.Header.Set(TokenHeader, testToken)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("fetch status = %d body = %s", rec.Code, rec.Body.String())
	}
	decoded, err := Decode(rec.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if decoded.TenantRef != "ref" || decoded.ConfigVersion != 4 {
		t.Fatalf("decoded=%+v", decoded)
	}
	if err := decoded.Verify(signer.PublicKey()); err != nil {
		t.Fatalf("fetched bundle signature invalid: %v", err)
	}
}

func TestClientPullsFromControlPlane(t *testing.T) {
	_, bundle := signed(t, transportSnapshot(7))
	hub := NewHub()
	hub.Publish(bundle.Snapshot, bundle)

	server := httptest.NewServer(mount(NewHandler(hub, testToken)))
	defer server.Close()

	client := NewClient(server.URL, testToken, nil)
	pulled, err := client.Fetch(context.Background(), "ref")
	if err != nil {
		t.Fatal(err)
	}
	if pulled.ConfigVersion != 7 || pulled.Snapshot == nil || pulled.Snapshot.TenantRef != "ref" {
		t.Fatalf("pulled=%+v", pulled)
	}

	refs, err := client.ListTenantRefs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0] != "ref" {
		t.Fatalf("tenant refs = %v", refs)
	}
}

func TestClientRejectsMissingTenant(t *testing.T) {
	hub := NewHub()
	server := httptest.NewServer(mount(NewHandler(hub, testToken)))
	defer server.Close()

	client := NewClient(server.URL, testToken, nil)
	if _, err := client.Fetch(context.Background(), "nope"); err == nil {
		t.Fatal("missing tenant fetch succeeded")
	} else if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected status error containing 404, got: %v", err)
	}
}
