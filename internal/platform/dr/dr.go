// Package dr provides a deterministic DR Runbook checklist (drill validation,
// read-only) with ordered steps, per-step pass/fail status and RTO/RPO hints,
// and fail-fast execution.
package dr

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/F31/liteAIG/internal/platform/coordination"
)

// StepName is a fixed runbook step.
type StepName string

// Ordered DR runbook steps.
const (
	StepValidateRuntime     StepName = "validate_runtime"
	StepReconcileAccounting StepName = "reconcile_accounting"
	StepReconcileBudget     StepName = "reconcile_budget"
	StepSwitchTraffic       StepName = "switch_traffic"
	StepVerifyReadiness     StepName = "verify_readiness"
)

// Fixed order of the runbook.
var Order = []StepName{
	StepValidateRuntime,
	StepReconcileAccounting,
	StepReconcileBudget,
	StepSwitchTraffic,
	StepVerifyReadiness,
}

// Env carries inputs a step needs to evaluate.
type Env struct {
	Region          string
	TenantID        string
	BudgetMode      coordination.BudgetConsistency
	TrafficSwitched bool
}

// StepResult is one step's outcome.
type StepResult struct {
	Step   StepName
	Passed bool
	Reason string
	RTO    time.Duration
	RPO    time.Duration
}

// Checklist executes runbook steps in order, halting on the first failure.
type Checklist struct {
	// Validators let the caller supply deterministic pass/fail per step.
	Validators map[StepName]func(context.Context, Env) error
}

// Readiness reports whether a Region is ready for a tenant on a ready node.
type Readiness func(context.Context, string, string, bool) bool

// Status is the read-only result exposed by a DR drill.
type Status struct {
	Region     string
	TenantID   string
	BudgetMode coordination.BudgetConsistency
	Ready      bool
	Steps      []StepResult
}

// NewChecklist builds a runbook with default always-capable validators.
func NewChecklist(validators map[StepName]func(context.Context, Env) error) *Checklist {
	if validators == nil {
		validators = map[StepName]func(context.Context, Env) error{}
	}
	return &Checklist{Validators: validators}
}

// Run executes the runbook in fixed order and stops at the first failure.
func (c *Checklist) Run(ctx context.Context, env Env) ([]StepResult, error) {
	var results []StepResult
	for _, step := range Order {
		result := StepResult{Step: step, RTO: rtoHint(step), RPO: rpoHint(step)}
		validator := c.Validators[step]
		if validator != nil {
			if err := validator(ctx, env); err != nil {
				result.Passed = false
				result.Reason = err.Error()
				results = append(results, result)
				return results, fmt.Errorf("runbook failed at step %s: %w", step, err)
			}
		}
		result.Passed = true
		result.Reason = "ok"
		results = append(results, result)
	}
	return results, nil
}

// View runs the read-only checklist and returns Region readiness together with
// the budget consistency mode used for the drill.
func (c *Checklist) View(ctx context.Context, env Env, nodeReady bool, readiness Readiness) (Status, error) {
	if env.Region == "" || env.TenantID == "" {
		return Status{}, ErrNoEnv
	}
	validators := make(map[StepName]func(context.Context, Env) error, len(c.Validators)+1)
	for step, validator := range c.Validators {
		validators[step] = validator
	}
	original := validators[StepVerifyReadiness]
	validators[StepVerifyReadiness] = func(ctx context.Context, env Env) error {
		if original != nil {
			if err := original(ctx, env); err != nil {
				return err
			}
		}
		if readiness == nil || !readiness(ctx, env.Region, env.TenantID, nodeReady) {
			return errors.New("region is not DR-ready")
		}
		return nil
	}
	steps, err := NewChecklist(validators).Run(ctx, env)
	status := Status{
		Region: env.Region, TenantID: env.TenantID, BudgetMode: env.BudgetMode,
		Ready: err == nil, Steps: append([]StepResult(nil), steps...),
	}
	return status, err
}

// rtoHint returns a conservative RTO hint per step (drill, not a promise).
func rtoHint(step StepName) time.Duration {
	switch step {
	case StepValidateRuntime:
		return 30 * time.Second
	case StepReconcileAccounting, StepReconcileBudget:
		return 5 * time.Minute
	case StepSwitchTraffic:
		return time.Minute
	case StepVerifyReadiness:
		return time.Minute
	}
	return 0
}

func rpoHint(step StepName) time.Duration {
	if step == StepReconcileAccounting || step == StepReconcileBudget {
		return time.Minute
	}
	return 0
}

var ErrNoEnv = errors.New("DR region and tenant are required")
