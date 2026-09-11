package dr

import (
	"context"
	"errors"
	"testing"

	"github.com/F31/liteAIG/internal/platform/coordination"
)

type recorder struct{ order []StepName }

func (r *recorder) step(name StepName) func(context.Context, Env) error {
	return func(context.Context, Env) error {
		r.order = append(r.order, name)
		return nil
	}
}

func TestRunbookExecutesInOrder(t *testing.T) {
	rec := &recorder{}
	checklist := NewChecklist(map[StepName]func(context.Context, Env) error{
		StepValidateRuntime:     rec.step(StepValidateRuntime),
		StepReconcileAccounting: rec.step(StepReconcileAccounting),
		StepReconcileBudget:     rec.step(StepReconcileBudget),
		StepSwitchTraffic:       rec.step(StepSwitchTraffic),
		StepVerifyReadiness:     rec.step(StepVerifyReadiness),
	})
	results, err := checklist.Run(context.Background(), Env{Region: "region-a", TenantID: "tenant", BudgetMode: coordination.BudgetGlobalSoft})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != len(Order) {
		t.Fatalf("results = %d, want %d", len(results), len(Order))
	}
	for i, want := range Order {
		if !results[i].Passed || results[i].Step != want {
			t.Fatalf("step %d = %+v, want %s", i, results[i], want)
		}
	}
	for i, step := range Order {
		if rec.order[i] != step {
			t.Fatalf("execution order = %v", rec.order)
		}
	}
	// Budget mode is visible on the env.
	if results[0].Step != StepValidateRuntime {
		t.Fatalf("first step = %s", results[0].Step)
	}
}

func TestRunbookHaltsOnFailure(t *testing.T) {
	rec := &recorder{}
	failing := errors.New("validation failed")
	checklist := NewChecklist(map[StepName]func(context.Context, Env) error{
		StepValidateRuntime:     rec.step(StepValidateRuntime),
		StepReconcileAccounting: func(context.Context, Env) error { return failing },
		StepReconcileBudget:     rec.step(StepReconcileBudget),
	})
	results, err := checklist.Run(context.Background(), Env{Region: "region-a", TenantID: "tenant"})
	if err == nil {
		t.Fatal("expected fail-fast error")
	}
	// The failing step and any prior completed steps are recorded; later steps
	// never run (ReconcileBudget is absent from results).
	if len(rec.order) != 1 {
		t.Fatalf("steps executed after failure: %v", rec.order)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	if results[1].Step != StepReconcileAccounting || results[1].Passed {
		t.Fatalf("failing step = %+v", results[1])
	}
}

func TestRunbookIsReadOnly(t *testing.T) {
	checklist := NewChecklist(nil) // no validators → all pass (drill)
	results, err := checklist.Run(context.Background(), Env{Region: "region-a", TenantID: "tenant", BudgetMode: coordination.BudgetRegional})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if !result.Passed {
			t.Fatalf("read-only drill step failed: %+v", result)
		}
	}
}

func TestRunbookViewIncludesReadinessAndBudgetMode(t *testing.T) {
	checklist := NewChecklist(nil)
	ready := func(context.Context, string, string, bool) bool { return true }
	status, err := checklist.View(context.Background(), Env{
		Region: "region-a", TenantID: "tenant", BudgetMode: coordination.BudgetGlobalSoft,
	}, true, ready)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Ready || status.Region != "region-a" || status.TenantID != "tenant" {
		t.Fatalf("status = %+v", status)
	}
	if status.BudgetMode != coordination.BudgetGlobalSoft {
		t.Fatalf("budget mode = %q", status.BudgetMode)
	}
	status.Steps[0].Reason = "mutated"
	if status.Steps[1].Reason != "ok" {
		t.Fatal("status steps should be independent values")
	}
}

func TestRunbookViewFailsWhenRegionIsNotReady(t *testing.T) {
	checklist := NewChecklist(nil)
	notReady := func(context.Context, string, string, bool) bool { return false }
	status, err := checklist.View(context.Background(), Env{
		Region: "region-a", TenantID: "tenant", BudgetMode: coordination.BudgetGlobalHard,
	}, true, notReady)
	if err == nil || status.Ready {
		t.Fatalf("status = %+v, err = %v", status, err)
	}
	last := status.Steps[len(status.Steps)-1]
	if last.Step != StepVerifyReadiness || last.Passed || last.RTO == 0 {
		t.Fatalf("readiness result = %+v", last)
	}
}
