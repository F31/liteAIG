package lkg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func verifyTestBundle(bundle *Bundle) error {
	if bundle.PayloadChecksum != "checksum" {
		return errors.New("invalid test signature")
	}
	return nil
}

func TestRegionIsolation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	regionStore, err := NewRegionStore(root, verifyTestBundle)
	if err != nil {
		t.Fatal(err)
	}
	// Region A saves a valid bundle; Region B has none.
	if err := regionStore.Save(ctx, "region-a", "ref", testBundle(4)); err != nil {
		t.Fatal(err)
	}
	if !regionStore.Ready(ctx, "region-a", "ref") {
		t.Fatal("region-a should be DR-ready")
	}
	if regionStore.RegionReady(ctx, "region-a", "ref", false) {
		t.Fatal("region-a must not be DR-ready when the node is not ready")
	}
	if !regionStore.RegionReady(ctx, "region-a", "ref", true) {
		t.Fatal("region-a should be DR-ready when the node and bundle are ready")
	}
	if regionStore.Ready(ctx, "region-b", "ref") {
		t.Fatal("region-b should not be DR-ready without a bundle")
	}
	// Corrupt region-a's active bundle: it falls back to previous and stays ready
	// if a previous exists; otherwise DR readiness fails for that tenant.
	if err := os.WriteFile(filepath.Join(root, "regions", "region-a", "ref", "active.bundle"), []byte("corrupt"), 0o640); err != nil {
		t.Fatal(err)
	}
	// No previous bundle → DR readiness fails.
	if regionStore.Ready(ctx, "region-a", "ref") {
		t.Fatal("region-a DR readiness must fail with a corrupt active and no previous")
	}
	// Region-b is unaffected.
	if regionStore.Ready(ctx, "region-b", "ref") {
		t.Fatal("region-b must remain not-ready independent of region-a")
	}
}

func TestRegionReadyWithPreviousFallback(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	regionStore, err := NewRegionStore(root, verifyTestBundle)
	if err != nil {
		t.Fatal(err)
	}
	store, err := regionStore.storeFor("region-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := regionStore.Save(ctx, "region-a", "ref", testBundle(4)); err != nil {
		t.Fatal(err)
	}
	if err := regionStore.Save(ctx, "region-a", "ref", testBundle(5)); err != nil {
		t.Fatal(err)
	}
	// Corrupt active; previous (v4) is valid → still DR-ready via fallback.
	active := filepath.Join(root, "regions", "region-a", "ref", "active.bundle")
	if err := os.WriteFile(active, []byte("corrupt"), 0o640); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadActive(ctx, "ref")
	if err != nil || loaded.ConfigVersion != 4 {
		t.Fatalf("previous fallback = %+v, %v", loaded, err)
	}
	if !regionStore.Ready(ctx, "region-a", "ref") {
		t.Fatal("region-a should be DR-ready via previous fallback")
	}
}

func TestRegionReadyFallsBackFromUnverifiedActive(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	regionStore, err := NewRegionStore(root, verifyTestBundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := regionStore.Save(ctx, "region-a", "ref", testBundle(4)); err != nil {
		t.Fatal(err)
	}
	invalid := testBundle(5)
	invalid.PayloadChecksum = "tampered"
	if err := regionStore.Save(ctx, "region-a", "ref", invalid); err != nil {
		t.Fatal(err)
	}
	if !regionStore.RegionReady(ctx, "region-a", "ref", true) {
		t.Fatal("valid previous bundle should keep the Region ready")
	}
}
