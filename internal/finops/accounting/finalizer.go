package accounting

import (
	"context"
	"errors"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"time"
)

type FinalizerConfig struct{ Timeout time.Duration }

func NewFinalizer(config FinalizerConfig, repository Repository, budget Budget, sink contracts.EventSink, facts Facts) (*Finalizer, error) {
	if config.Timeout <= 0 || repository == nil {
		return nil, errors.New("finalizer timeout and repository are required")
	}
	return &Finalizer{repository: repository, budget: budget, sink: sink, facts: facts, timeout: config.Timeout}, nil
}
func (f *Finalizer) Finalize(ctx context.Context) (Result, error) {
	f.once.Do(func() {
		finalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), f.timeout)
		defer cancel()
		// The reservation must never leak: if persistence or the settle step
		// below fails before the reservation is finalized, the deferred
		// release frees the reserved tokens.
		settled := false
		defer func() {
			if f.budget != nil && f.facts.ReservationID != "" && !settled {
				_ = f.budget.Release(f.facts.BudgetPolicyID, f.facts.ReservationID)
			}
		}()
		f.result.Created, f.err = f.repository.Finalize(finalCtx, f.facts)
		if f.err != nil {
			return
		}
		if f.budget != nil && f.facts.ReservationID != "" {
			if f.facts.ActualTokens == nil {
				f.err = f.budget.Release(f.facts.BudgetPolicyID, f.facts.ReservationID)
			} else {
				f.err = f.budget.Reconcile(f.facts.BudgetPolicyID, f.facts.ReservationID, *f.facts.ActualTokens)
			}
			settled = f.err == nil
			if f.err != nil {
				return
			}
		}
		if f.sink != nil {
			f.result.TelemetryError = f.sink.Emit(finalCtx, contracts.DomainEvent{ID: f.facts.UsageEventID, Kind: "request.completed", OccurredAt: f.facts.CompletedAt, TenantID: f.facts.TenantID, ProjectID: f.facts.ProjectID, RequestID: f.facts.RequestID, Attributes: map[string]string{"outcome": f.facts.Outcome, "logical_model": f.facts.LogicalModel, "deployment_id": f.facts.DeploymentID}})
		}
	})
	return f.result, f.err
}
