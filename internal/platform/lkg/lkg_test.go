package lkg

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

func testBundle(version int64) Bundle {
	return Bundle{
		SchemaVersion: "v1", TenantID: "tenant", TenantRef: "ref",
		ConfigVersion: version, SecurityEpoch: version,
		PayloadChecksum: "checksum",
		SnapshotData:    &runtime.TenantSnapshotData{TenantID: "tenant", TenantRef: "ref", Version: version},
	}
}

func TestSaveAndLoadActive(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "ref", testBundle(4)); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadActive(ctx, "ref")
	if err != nil || loaded.ConfigVersion != 4 {
		t.Fatalf("LoadActive() = %+v, %v", loaded, err)
	}
}

func TestSaveRotatesToPreviousAndCorruptActiveFallsBack(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "ref", testBundle(4)); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "ref", testBundle(5)); err != nil {
		t.Fatal(err)
	}
	// Corrupt the active bundle so the node must fall back to previous (v4).
	if err := os.WriteFile(filepath.Join(dir, "ref", "active.bundle"), []byte("corrupt"), 0o640); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadActive(ctx, "ref")
	if err != nil || loaded.ConfigVersion != 4 {
		t.Fatalf("corrupt-active fallback = %+v, %v", loaded, err)
	}
}

func TestBootFromLKGWhenControlPlaneUnreachable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "ref", testBundle(7)); err != nil {
		t.Fatal(err)
	}
	// Control Plane unreachable: boot from the local LKG.
	loaded, err := store.LoadActive(ctx, "ref")
	if err != nil || loaded.ConfigVersion != 7 || loaded.SnapshotData.Version != 7 {
		t.Fatalf("LKG boot = %+v, %v", loaded, err)
	}
}

func TestMissingLKGIsError(t *testing.T) {
	ctx := context.Background()
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadActive(ctx, "missing"); err == nil {
		t.Fatal("missing LKG should be an error")
	}
}

func TestReadinessCouplesToBundleValidity(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if store.Ready(ctx, "ref") {
		t.Fatal("ready without any bundle")
	}
	if err := store.Save(ctx, "ref", testBundle(1)); err != nil {
		t.Fatal(err)
	}
	if !store.Ready(ctx, "ref") {
		t.Fatal("not ready with a valid active bundle")
	}
	// Corrupt the active bundle → not ready (falls back to missing previous).
	if err := os.WriteFile(filepath.Join(dir, "ref", "active.bundle"), []byte("corrupt"), 0o640); err != nil {
		t.Fatal(err)
	}
	if store.Ready(ctx, "ref") {
		t.Fatal("ready with a corrupt active bundle")
	}
}
