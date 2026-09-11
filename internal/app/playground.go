package app

import (
	"context"
	"github.com/F31/liteAIG/internal/controlplane/backend"

	"github.com/F31/liteAIG/internal/gateway/playground"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/tenancy"
)

// litePlayground executes test-principal requests through the real Lite
// pipeline (routing + execution + accounting + live bus).
type litePlayground struct {
	service  *playground.Service
	registry *runtime.ActiveRegistry
	tenancy  tenancy.Repository
}

func (p *litePlayground) Run(ctx context.Context, scope tenancy.TenantScope, input backend.PlaygroundRequest) (backend.PlaygroundResponse, error) {
	return p.invoke(ctx, scope, input, nil)
}

// PlaygroundStream runs a streaming playground invocation, forwarding each
// normalized token to emit as it arrives from the provider.
func (p *litePlayground) PlaygroundStream(ctx context.Context, scope tenancy.TenantScope, input backend.PlaygroundRequest, emit func(backend.StreamEventView)) (backend.PlaygroundResponse, error) {
	onStream := func(event interaction.StreamEvent) {}
	if emit != nil {
		onStream = func(event interaction.StreamEvent) {
			emit(backend.StreamEventView{Delta: event.Delta, StopReason: event.StopReason, Final: event.Final})
		}
	}
	return p.invoke(ctx, scope, input, onStream)
}

func (p *litePlayground) invoke(ctx context.Context, scope tenancy.TenantScope, input backend.PlaygroundRequest, onStream func(interaction.StreamEvent)) (backend.PlaygroundResponse, error) {
	snapshot, ok := p.registry.TenantByID(scope.TenantID)
	if !ok {
		return backend.PlaygroundResponse{}, tenancy.ErrNotFound
	}
	projectID := ""
	if tenant, err := p.tenancy.GetTenant(ctx, scope); err == nil {
		projectID = tenant.DefaultProjectID
	}
	result, err := p.service.Run(ctx, playground.Input{
		Snapshot: snapshot, TenantID: scope.TenantID, ProjectID: projectID,
		Model: input.Model, Input: input.Input, Stream: input.Stream, OnStream: onStream,
	})
	if err != nil {
		return backend.PlaygroundResponse{}, err
	}
	cost := result.Cost
	return backend.PlaygroundResponse{
		RequestID:          result.RequestID,
		Output:             result.Output,
		SelectedDeployment: result.SelectedDeployment,
		InputTokens:        result.Usage.InputTokens,
		OutputTokens:       result.Usage.OutputTokens,
		Cost:               cost,
		LatencyMS:          result.LatencyMS,
	}, nil
}

// liteFirstCall records Setup's first call through the same real pipeline.
type liteFirstCall struct {
	service  *playground.Service
	registry *runtime.ActiveRegistry
}

func (p *liteFirstCall) RunFirstCall(ctx context.Context, tenantID, projectID, model string) (string, int64, int64, error) {
	snapshot, ok := p.registry.TenantByID(tenantID)
	if !ok {
		return "", 0, 0, tenancy.ErrNotFound
	}
	result, err := p.service.Run(ctx, playground.Input{
		Snapshot: snapshot, TenantID: tenantID, ProjectID: projectID,
		Model: model, Input: "hello", Stream: false,
	})
	if err != nil {
		return "", 0, 0, err
	}
	return result.RequestID, result.Usage.InputTokens, result.Usage.OutputTokens, nil
}
