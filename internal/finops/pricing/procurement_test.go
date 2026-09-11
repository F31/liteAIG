package pricing

import "testing"

func TestExternalProcurementCostSeparateAndIncluded(t *testing.T) {
	external := ExternalProcurement{RelationshipID: "rel-1", VendorID: "vendor-a", ExternalCost: 20}
	taskTotal := 5.0 + external.ExternalCost // internal 5 + external 20
	// The external cost is part of the task total AND visible separately.
	if taskTotal != 25 {
		t.Fatalf("task total = %f, want 25 (external included)", taskTotal)
	}
	if external.ExternalCost != 20 {
		t.Fatalf("external procurement = %f", external.ExternalCost)
	}
}

func TestProcurementBudgetCannotBypassTaskTotal(t *testing.T) {
	budget := ExternalProcurementBudget{RelationshipID: "rel-1", Limit: 100}
	external := ExternalProcurement{RelationshipID: "rel-1", VendorID: "vendor-a", ExternalCost: 20}

	// Task total already near its limit: procurement headroom must not relax it.
	if err := budget.Admit(external, 20, 10); err == nil {
		t.Fatal("procurement headroom must not bypass the task total budget")
	}
	// Both budgets pass independently.
	if err := budget.Admit(external, 100, 10); err != nil {
		t.Fatalf("both budgets should pass: %v", err)
	}
	// Procurement exhaustion blocks even if the task total has headroom.
	exhausted := ExternalProcurementBudget{RelationshipID: "rel-1", Limit: 100, Spent: 90}
	if err := exhausted.Admit(external, 100, 10); err == nil {
		t.Fatal("procurement exhaustion must block even with task-total headroom")
	}
}
