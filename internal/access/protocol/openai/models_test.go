package openai

import (
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"testing"
)

func TestListModelsFiltersByKeyAndHidesPhysicalResources(t *testing.T) {
	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{LogicalModels: []runtime.LogicalModel{{ID: "physical-secret-id", Alias: "allowed"}, {ID: "other", Alias: "denied"}}})
	response := ListModels(snapshot, runtime.APIKey{ModelAllowlist: []string{"allowed"}})
	if len(response.Data) != 1 || response.Data[0].ID != "allowed" || response.Data[0].OwnedBy != "liteaig" {
		t.Fatalf("ListModels()=%+v", response)
	}
}
