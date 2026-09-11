package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/F31/liteAIG/internal/kernel"
)

// Directive controls only the fixed cache-compatible short circuit.
type Directive uint8

const (
	Continue Directive = iota
	SkipResolutionExecution
)

// Handler implements one fixed pipeline stage.
type Handler interface {
	Handle(context.Context, *kernel.RequestContext) (Directive, error)
}

// HandlerFunc adapts a function to a Handler.
type HandlerFunc func(context.Context, *kernel.RequestContext) (Directive, error)

func (f HandlerFunc) Handle(ctx context.Context, request *kernel.RequestContext) (Directive, error) {
	return f(ctx, request)
}

// Handlers names every stage so callers cannot add or reorder stages.
type Handlers struct {
	Admission              Handler
	InputGuardrail         Handler
	PolicyCostPreflight    Handler
	Resolution             Handler
	ExecutionResilience    Handler
	OutputStreamGuardrail  Handler
	AccountingAndTelemetry Handler
}

// Runner executes the compile-time-fixed seven-stage pipeline.
type Runner struct {
	handlers Handlers
}

// NewRunner rejects incomplete pipelines before they can serve traffic.
func NewRunner(handlers Handlers) (*Runner, error) {
	required := []struct {
		stage   Stage
		handler Handler
	}{
		{Admission, handlers.Admission},
		{InputGuardrail, handlers.InputGuardrail},
		{PolicyCostPreflight, handlers.PolicyCostPreflight},
		{Resolution, handlers.Resolution},
		{ExecutionResilience, handlers.ExecutionResilience},
		{OutputStreamGuardrail, handlers.OutputStreamGuardrail},
		{AccountingTelemetry, handlers.AccountingAndTelemetry},
	}
	for _, item := range required {
		if item.handler == nil {
			return nil, fmt.Errorf("pipeline stage %s has no handler", item.stage)
		}
	}
	return &Runner{handlers: handlers}, nil
}

// Run executes all applicable stages and always invokes Accounting and Telemetry once.
func (r *Runner) Run(ctx context.Context, request *kernel.RequestContext) (err error) {
	defer func() {
		request.Err = err
		_, accountingErr := r.handlers.AccountingAndTelemetry.Handle(ctx, request)
		err = errors.Join(err, accountingErr)
	}()

	if err = runStage(ctx, request, Admission, r.handlers.Admission); err != nil {
		return err
	}
	if err = runStage(ctx, request, InputGuardrail, r.handlers.InputGuardrail); err != nil {
		return err
	}

	directive, preflightErr := r.handlers.PolicyCostPreflight.Handle(ctx, request)
	if preflightErr != nil {
		return fmt.Errorf("pipeline stage %s: %w", PolicyCostPreflight, preflightErr)
	}
	if directive != Continue && directive != SkipResolutionExecution {
		return fmt.Errorf("pipeline stage %s returned unsupported directive %d", PolicyCostPreflight, directive)
	}

	if directive == Continue {
		if err = runStage(ctx, request, Resolution, r.handlers.Resolution); err != nil {
			return err
		}
		if err = runStage(ctx, request, ExecutionResilience, r.handlers.ExecutionResilience); err != nil {
			return err
		}
	}

	return runStage(ctx, request, OutputStreamGuardrail, r.handlers.OutputStreamGuardrail)
}

func runStage(
	ctx context.Context,
	request *kernel.RequestContext,
	stage Stage,
	handler Handler,
) error {
	directive, err := handler.Handle(ctx, request)
	if err != nil {
		return fmt.Errorf("pipeline stage %s: %w", stage, err)
	}
	if directive != Continue {
		return fmt.Errorf("pipeline stage %s returned unsupported directive %d", stage, directive)
	}
	return nil
}
