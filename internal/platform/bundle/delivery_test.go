package bundle

import (
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

const testSchema = "v1"

func deliverySnapshot(version int64) *runtime.TenantRuntimeSnapshot {
	return runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref", Status: "active", Version: version, SecurityEpoch: 3,
		PublishedAt: time.Unix(10, 0),
		Credentials: []runtime.Credential{{ID: "c", ProviderID: "p", SecretRef: "secret://tenant/c", Status: "enabled"}},
	})
}

func TestSignerAndWireRoundTrip(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer := NewSigner(privateKey, testSchema, 2)
	snapshot := deliverySnapshot(4)
	signed, err := signer.Sign(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := signed.Verify(signer.PublicKey()); err != nil {
		t.Fatal(err)
	}
	wire, err := signed.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(wire)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.TenantRef != "ref" || decoded.ConfigVersion != 4 || decoded.SchemaVersion != testSchema {
		t.Fatalf("decoded bundle lost fields: %+v", decoded)
	}
	if decoded.Snapshot == nil || decoded.Snapshot.Version != 4 || decoded.Snapshot.TenantRef != "ref" {
		t.Fatalf("decoded snapshot not rebuilt: %+v", decoded.Snapshot)
	}
	if err := decoded.Verify(signer.PublicKey()); err != nil {
		t.Fatalf("wire round-trip invalidates signature: %v", err)
	}
}

func TestWireTamperDetected(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(nil)
	signer := NewSigner(privateKey, testSchema, 2)
	signed, err := signer.Sign(deliverySnapshot(4))
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := signed.Encode()
	decoded, err := Decode(wire)
	if err != nil {
		t.Fatal(err)
	}
	// Tamper with the rebuilt snapshot after decoding.
	decoded.Snapshot.TenantID = "other-tenant"
	if err := decoded.Verify(signer.PublicKey()); err == nil {
		t.Fatal("tampered bundle verified")
	}
}

func TestVerifyPublicKeyParsing(t *testing.T) {
	if _, err := VerifyPublicKey(""); err == nil {
		t.Fatal("empty key accepted")
	}
	_, privateKey, _ := ed25519.GenerateKey(nil)
	public := privateKey.Public().(ed25519.PublicKey)
	wirePublic, err := VerifyPublicKey(publicToHex(public))
	if err != nil {
		t.Fatal(err)
	}
	if string(wirePublic) != string(public) {
		t.Fatal("hex round-trip mismatch")
	}
}

func publicToHex(public ed25519.PublicKey) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(public)*2)
	for _, b := range public {
		out = append(out, digits[b>>4], digits[b&0xf])
	}
	return string(out)
}
