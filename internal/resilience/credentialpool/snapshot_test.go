package credentialpool

import (
	"testing"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

func snapshotWithPool() *runtime.TenantRuntimeSnapshot {
	return runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "ref",
		Providers: []runtime.Provider{
			{ID: "provider", OwnerScope: "TENANT_PRIVATE", Type: "openai", Status: "enabled"},
		},
		Credentials: []runtime.Credential{
			{ID: "cred-a", ProviderID: "provider", Status: "enabled"},
			{ID: "cred-b", ProviderID: "provider", Status: "enabled"},
			{ID: "cred-disabled", ProviderID: "provider", Status: "disabled"},
		},
		CredentialPools: []runtime.CredentialPool{{
			ID: "pool-1", TenantID: "tenant", ProviderID: "provider", Strategy: "weighted",
			Members: []runtime.PoolMember{
				{CredentialID: "cred-a", Weight: 1},
				{CredentialID: "cred-b", Weight: 5},
				{CredentialID: "cred-disabled", Weight: 1},
			},
		}},
	})
}

func TestFromSnapshotSelectsAndHonorsSnapshotState(t *testing.T) {
	snapshot := snapshotWithPool()
	deployment := runtime.Deployment{ID: "deployment", ProviderID: "provider", PoolID: "pool-1"}
	state := NewSnapshotState(snapshot, deployment.ID)
	pool, err := FromSnapshot(snapshot, deployment, state)
	if err != nil {
		t.Fatal(err)
	}
	order, err := pool.Order("key")
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 {
		t.Fatalf("order = %v (disabled member leaked into eligibility)", order)
	}
	for _, id := range order {
		if id == "cred-disabled" {
			t.Fatal("disabled credential was eligible")
		}
	}
}

func TestCircuitOpenExcludesCredential(t *testing.T) {
	snapshot := snapshotWithPool()
	deployment := runtime.Deployment{ID: "deployment", ProviderID: "provider", PoolID: "pool-1"}
	state := NewSnapshotState(snapshot, deployment.ID).
		WithCircuit(func(deploymentID, credentialID string) bool {
			return credentialID == "cred-b"
		})
	pool, err := FromSnapshot(snapshot, deployment, state)
	if err != nil {
		t.Fatal(err)
	}
	order, err := pool.Order("key")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range order {
		if id == "cred-b" {
			t.Fatal("circuit-open credential was eligible")
		}
	}
	if len(order) != 1 || order[0] != "cred-a" {
		t.Fatalf("order = %v", order)
	}
}

func TestDeploymentWithoutPoolUsesSingleCredential(t *testing.T) {
	snapshot := snapshotWithPool()
	deployment := runtime.Deployment{ID: "deployment", ProviderID: "provider", CredentialID: "cred-a"}
	state := NewSnapshotState(snapshot, deployment.ID)
	pool, err := FromSnapshot(snapshot, deployment, state)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := pool.Select("key")
	if err != nil || selected != "cred-a" {
		t.Fatalf("selected = %q, %v", selected, err)
	}
}

func TestPoolNotFoundInSnapshot(t *testing.T) {
	snapshot := snapshotWithPool()
	deployment := runtime.Deployment{ID: "deployment", ProviderID: "provider", PoolID: "missing"}
	_, err := FromSnapshot(snapshot, deployment, NewSnapshotState(snapshot, deployment.ID))
	if err == nil || err.Error() != "credential pool not found in snapshot" {
		t.Fatalf("error = %v", err)
	}
}
