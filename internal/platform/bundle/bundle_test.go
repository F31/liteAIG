package bundle

import (
	"strings"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

func testBundle() *RuntimeBundle {
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Version: 4, SecurityEpoch: 3, Status: "active",
		Credentials: []runtime.Credential{{ID: "c", ProviderID: "p", SecretRef: "secret://tenant/c", Status: "enabled"}},
	})
	return &RuntimeBundle{
		SchemaVersion: "v1", TenantID: "tenant", TenantRef: "ref",
		ConfigVersion: 4, SecurityEpoch: 3, SystemRuntimeVersion: 2,
		PublishedAt: time.Unix(10, 0), Snapshot: snapshot,
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	publicKey, privateKey, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	bundle := testBundle()
	if err := bundle.Sign(privateKey); err != nil {
		t.Fatal(err)
	}
	if bundle.PayloadChecksum == "" || len(bundle.Signature) == 0 {
		t.Fatal("signature or checksum not set")
	}
	if err := bundle.Verify(publicKey); err != nil {
		t.Fatalf("Verify() = %v", err)
	}
}

func TestTamperedPayloadRejected(t *testing.T) {
	publicKey, privateKey, _ := GenerateKey()
	bundle := testBundle()
	if err := bundle.Sign(privateKey); err != nil {
		t.Fatal(err)
	}
	// Tamper with the snapshot data.
	bundle.Snapshot.TenantID = "other-tenant"
	if err := bundle.Verify(publicKey); err == nil {
		t.Fatal("tampered payload verified")
	}
}

func TestKeyMismatchRejected(t *testing.T) {
	_, privateKey, _ := GenerateKey()
	otherPublic, _, _ := GenerateKey()
	bundle := testBundle()
	if err := bundle.Sign(privateKey); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Verify(otherPublic); err == nil {
		t.Fatal("bundle verified with a different key")
	}
}

func TestBundleCarriesNoPlaintextSecret(t *testing.T) {
	_, privateKey, _ := GenerateKey()
	bundle := testBundle()
	if err := bundle.Sign(privateKey); err != nil {
		t.Fatal(err)
	}
	payload, err := bundle.Payload()
	if err != nil {
		t.Fatal(err)
	}
	// The payload must not embed plaintext credential material. Credentials
	// remain behind secret_ref (not serialized by the bundle payload) and are
	// resolved server-side at invocation time.
	if strings.Contains(string(payload), "sk-live-") || strings.Contains(string(payload), "Bearer ") {
		t.Fatal("bundle payload leaked plaintext secret material")
	}
	if strings.Contains(string(payload), "super-secret-credential") {
		t.Fatal("bundle payload leaked credential plaintext")
	}
}
