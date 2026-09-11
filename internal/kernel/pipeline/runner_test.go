package pipeline

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/F31/liteAIG/internal/kernel"
)

func TestRunnerExecutesFixedOrder(t *testing.T) {
	var got []Stage
	runner := mustRunner(t, recordingHandlers(&got, Continue, nil, nil))

	if err := runner.Run(context.Background(), &kernel.RequestContext{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []Stage{
		Admission,
		InputGuardrail,
		PolicyCostPreflight,
		Resolution,
		ExecutionResilience,
		OutputStreamGuardrail,
		AccountingTelemetry,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stage order = %v, want %v", got, want)
	}
}

func TestRunnerCacheShortCircuitPreservesOutputAndAccounting(t *testing.T) {
	var got []Stage
	runner := mustRunner(t, recordingHandlers(&got, SkipResolutionExecution, nil, nil))

	if err := runner.Run(context.Background(), &kernel.RequestContext{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []Stage{
		Admission,
		InputGuardrail,
		PolicyCostPreflight,
		OutputStreamGuardrail,
		AccountingTelemetry,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stage order = %v, want %v", got, want)
	}
}

func TestRunnerFailureStillAccountsOnce(t *testing.T) {
	var got []Stage
	stageErr := errors.New("blocked")
	runner := mustRunner(t, recordingHandlers(&got, Continue, stageErr, nil))

	err := runner.Run(context.Background(), &kernel.RequestContext{})
	if !errors.Is(err, stageErr) {
		t.Fatalf("Run() error = %v, want blocked error", err)
	}
	want := []Stage{Admission, InputGuardrail, AccountingTelemetry}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stage order = %v, want %v", got, want)
	}
}

func TestRunnerJoinsAccountingFailure(t *testing.T) {
	var got []Stage
	stageErr := errors.New("blocked")
	accountingErr := errors.New("accounting unavailable")
	runner := mustRunner(t, recordingHandlers(&got, Continue, stageErr, accountingErr))

	err := runner.Run(context.Background(), &kernel.RequestContext{})
	if !errors.Is(err, stageErr) || !errors.Is(err, accountingErr) {
		t.Fatalf("Run() error = %v, want both failures", err)
	}
}

func TestNewRunnerRejectsIncompletePipeline(t *testing.T) {
	_, err := NewRunner(Handlers{})
	if err == nil || !strings.Contains(err.Error(), "admission") {
		t.Fatalf("NewRunner() error = %v", err)
	}
}

func TestRunnerRejectsSkipOutsidePreflight(t *testing.T) {
	handlers := recordingHandlers(nil, Continue, nil, nil)
	handlers.Admission = HandlerFunc(func(context.Context, *kernel.RequestContext) (Directive, error) {
		return SkipResolutionExecution, nil
	})
	runner := mustRunner(t, handlers)

	err := runner.Run(context.Background(), &kernel.RequestContext{})
	if err == nil || !strings.Contains(err.Error(), "unsupported directive") {
		t.Fatalf("Run() error = %v", err)
	}
}

func mustRunner(t *testing.T, handlers Handlers) *Runner {
	t.Helper()
	runner, err := NewRunner(handlers)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	return runner
}

func recordingHandlers(
	record *[]Stage,
	preflight Directive,
	inputErr error,
	accountingErr error,
) Handlers {
	handler := func(stage Stage, directive Directive, stageErr error) HandlerFunc {
		return func(context.Context, *kernel.RequestContext) (Directive, error) {
			if record != nil {
				*record = append(*record, stage)
			}
			return directive, stageErr
		}
	}
	return Handlers{
		Admission:              handler(Admission, Continue, nil),
		InputGuardrail:         handler(InputGuardrail, Continue, inputErr),
		PolicyCostPreflight:    handler(PolicyCostPreflight, preflight, nil),
		Resolution:             handler(Resolution, Continue, nil),
		ExecutionResilience:    handler(ExecutionResilience, Continue, nil),
		OutputStreamGuardrail:  handler(OutputStreamGuardrail, Continue, nil),
		AccountingAndTelemetry: handler(AccountingTelemetry, Continue, accountingErr),
	}
}
