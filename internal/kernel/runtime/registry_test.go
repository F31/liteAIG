package runtime

import (
	"sync"
	"testing"
)

func TestActiveRegistryAtomicallyReplacesTenant(t *testing.T) {
	registry := &ActiveRegistry{}
	first := NewTenantSnapshot(TenantSnapshotData{TenantID: "tenant", TenantRef: "ref", Version: 1})
	second := NewTenantSnapshot(TenantSnapshotData{TenantID: "tenant", TenantRef: "ref", Version: 2})
	registry.ActivateTenant("ref", first)
	pinned, _ := registry.Tenant("ref")

	var wait sync.WaitGroup
	for i := 0; i < 100; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for j := 0; j < 100; j++ {
				value, ok := registry.Tenant("ref")
				if !ok || value.Version < 1 || value.Version > 2 {
					t.Errorf("invalid snapshot: %+v", value)
					return
				}
			}
		}()
	}
	registry.ActivateTenant("ref", second)
	wait.Wait()
	if pinned.Version != 1 {
		t.Fatalf("pinned request snapshot changed to %d", pinned.Version)
	}
	active, _ := registry.Tenant("ref")
	if active.Version != 2 {
		t.Fatalf("active version = %d", active.Version)
	}
}
