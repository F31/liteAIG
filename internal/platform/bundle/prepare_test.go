package bundle

import (
	"errors"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

var errLocalRef = errors.New("local resource reference missing")

func TestPrepareAcksValidBundle(t *testing.T) {
	publicKey, privateKey, _ := GenerateKey()
	bundle := testBundle()
	if err := bundle.Sign(privateKey); err != nil {
		t.Fatal(err)
	}
	result := Prepare(bundle, publicKey, nil)
	if !result.Ack {
		t.Fatalf("valid bundle NACKed: %s", result.Reason)
	}
	if err := Activate(bundle, result.Ack); err != nil {
		t.Fatalf("Activate() = %v", err)
	}
}

func TestPrepareNacksTamperedOrInvalid(t *testing.T) {
	publicKey, privateKey, _ := GenerateKey()
	bundle := testBundle()
	if err := bundle.Sign(privateKey); err != nil {
		t.Fatal(err)
	}
	// Tampered snapshot → NACK.
	bundle.Snapshot.TenantID = "other"
	if result := Prepare(bundle, publicKey, nil); result.Ack {
		t.Fatal("tampered bundle ACKed")
	}
	if err := Activate(bundle, false); err == nil {
		t.Fatal("NACKed bundle activated")
	}
	// Missing metadata → NACK.
	invalid := &RuntimeBundle{Snapshot: testBundle().Snapshot}
	if result := Prepare(invalid, publicKey, nil); result.Ack {
		t.Fatal("incomplete bundle ACKed")
	}
}

func TestPrepareLocalValidatorNacks(t *testing.T) {
	publicKey, privateKey, _ := GenerateKey()
	bundle := testBundle()
	if err := bundle.Sign(privateKey); err != nil {
		t.Fatal(err)
	}
	validator := func(*RuntimeBundle) error { return errLocalRef }
	if result := Prepare(bundle, publicKey, validator); result.Ack {
		t.Fatal("local validator violation ACKed")
	}
}

func TestPrepareRejectsSnapshotlessActivation(t *testing.T) {
	empty := &RuntimeBundle{Snapshot: nil}
	if err := Activate(empty, true); err == nil {
		t.Fatal("snapshotless bundle activated")
	}
}

func TestSnapshotCarriedInBundle(t *testing.T) {
	_, privateKey, _ := GenerateKey()
	bundle := testBundle()
	if err := bundle.Sign(privateKey); err != nil {
		t.Fatal(err)
	}
	if bundle.Snapshot == nil || bundle.Snapshot.Version != 4 {
		t.Fatalf("bundle snapshot = %+v", bundle.Snapshot)
	}
	_ = runtime.NewTenantSnapshot(runtime.TenantSnapshotData{})
}
