package golden

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/platform/coordination"
	"github.com/F31/liteAIG/internal/platform/dr"
	"github.com/F31/liteAIG/internal/platform/lkg"
)

func TestScenarioGRegionDRIsolationAndRunbook(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	regionStore, err := lkg.NewRegionStore(root, func(bundle *lkg.Bundle) error {
		if bundle.PayloadChecksum != "verified" {
			return errors.New("bundle verification failed")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Region A has a valid LKG; Region B does not.
	if err := regionStore.Save(ctx, "region-a", "ref", lkg.Bundle{
		TenantRef: "ref", ConfigVersion: 4, PayloadChecksum: "verified",
		SnapshotData: &runtime.TenantSnapshotData{TenantID: "tenant", TenantRef: "ref", Version: 4},
	}); err != nil {
		t.Fatal(err)
	}
	if !regionStore.Ready(ctx, "region-a", "ref") {
		t.Fatal("region-a should be DR-ready")
	}
	if regionStore.Ready(ctx, "region-b", "ref") {
		t.Fatal("region-b should not be DR-ready")
	}
	status, err := dr.NewChecklist(nil).View(ctx, dr.Env{
		Region: "region-a", TenantID: "ref", BudgetMode: coordination.BudgetGlobalSoft,
	}, true, regionStore.RegionReady)
	if err != nil || !status.Ready || status.BudgetMode != coordination.BudgetGlobalSoft {
		t.Fatalf("DR status = %+v, %v", status, err)
	}

	// Corrupt region-a's active bundle with no previous → DR readiness fails.
	if err := os.WriteFile(filepath.Join(root, "regions", "region-a", "ref", "active.bundle"), []byte("corrupt"), 0o640); err != nil {
		t.Fatal(err)
	}
	if regionStore.Ready(ctx, "region-a", "ref") {
		t.Fatal("region-a DR readiness must fail on a corrupt active with no previous")
	}

	// DR runbook is deterministic and fail-fast.
	failing := errors.New("accounting out of sync")
	checklist := dr.NewChecklist(map[dr.StepName]func(context.Context, dr.Env) error{
		dr.StepValidateRuntime:     func(context.Context, dr.Env) error { return nil },
		dr.StepReconcileAccounting: func(context.Context, dr.Env) error { return failing },
	})
	results, err := checklist.Run(ctx, dr.Env{Region: "region-a", TenantID: "tenant", BudgetMode: coordination.BudgetGlobalSoft})
	if err == nil {
		t.Fatal("expected fail-fast runbook error")
	}
	// The failing step is recorded and the runbook halted.
	if len(results) != 2 || results[1].Step != dr.StepReconcileAccounting || results[1].Passed {
		t.Fatalf("runbook results = %+v", results)
	}
	// Budget mode was carried by the env (world-state view is intact).
	for _, result := range results[:1] {
		if !result.Passed {
			t.Fatalf("validation step failed: %+v", result)
		}
	}
}
