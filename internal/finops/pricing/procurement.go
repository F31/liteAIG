// Package pricing owns pricing, chargeback, and external procurement cost.
package pricing

import "errors"

// ExternalProcurement is the distinct external-agent cost dimension.
type ExternalProcurement struct {
	RelationshipID string
	VendorID       string
	ExternalCost   float64
}

// ExternalProcurementBudget bounds external spend per relationship/vendor.
type ExternalProcurementBudget struct {
	RelationshipID string
	Limit          float64
	Spent          float64
}

// Admit evaluates an external call against BOTH the task total budget and the
// procurement budget. The procurement budget must never be usable to bypass a
// task-level cost limit: both must pass independently.
func (b ExternalProcurementBudget) Admit(external ExternalProcurement, taskTotalLimit float64, taskTotalSpent float64) error {
	if taskTotalSpent+external.ExternalCost > taskTotalLimit {
		return errors.New("task total budget exceeded")
	}
	if b.Spent+external.ExternalCost > b.Limit {
		return errors.New("external procurement budget exceeded")
	}
	return nil
}

// Commit records external spend on the procurement budget.
func (b *ExternalProcurementBudget) Commit(external ExternalProcurement) {
	b.Spent += external.ExternalCost
}
