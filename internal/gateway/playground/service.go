// Package playground executes test-principal requests through the production pipeline.
package playground

import (
	"context"
	"errors"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

type Pipeline interface {
	Run(context.Context, *kernel.RequestContext) error
}
type Input struct {
	Snapshot                          *runtime.TenantRuntimeSnapshot
	TenantID, ProjectID, Model, Input string
	Stream                            bool
	// OnStream receives normalized stream events live when Stream is true.
	OnStream func(interaction.StreamEvent)
}

type streamCallback struct {
	on func(interaction.StreamEvent)
}

func (s streamCallback) WriteChunk(_ context.Context, chunk contracts.StreamChunk) error {
	if s.on != nil {
		s.on(chunk.Event)
	}
	return nil
}

type Result struct {
	RequestID, Output, SelectedDeployment string
	Usage                                 interaction.UnifiedUsage
	Cost                                  *float64
	LatencyMS                             int64
}
type Service struct {
	pipeline Pipeline
	ids      contracts.IDGenerator
	clock    contracts.Clock
}

type RequestError struct {
	RequestID string
	Err       error
}

func (e *RequestError) Error() string { return e.Err.Error() }
func (e *RequestError) Unwrap() error { return e.Err }

func New(pipeline Pipeline, ids contracts.IDGenerator, clock contracts.Clock) (*Service, error) {
	if pipeline == nil || ids == nil || clock == nil {
		return nil, errors.New("playground dependencies are required")
	}
	return &Service{pipeline: pipeline, ids: ids, clock: clock}, nil
}
func (s *Service) Run(ctx context.Context, input Input) (*Result, error) {
	requestID, err := s.ids.New()
	if err != nil {
		return nil, err
	}
	received := s.clock.Now()
	request := &kernel.RequestContext{RequestID: requestID, ReceivedAt: received, Snapshot: input.Snapshot, Source: "playground", Interaction: &interaction.Context{Kind: interaction.KindModel, Protocol: "playground", TenantID: input.TenantID, ProjectID: input.ProjectID, Caller: interaction.PrincipalRef{Type: "test_principal", ID: requestID}, Target: interaction.ResourceRef{Type: "logical_model", ID: input.Model}}, Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: input.Model, Stream: input.Stream, Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: input.Input}}}}}
	if input.Stream && input.OnStream != nil {
		request.StreamWriter = streamCallback{on: input.OnStream}
	}
	if err := s.pipeline.Run(ctx, request); err != nil {
		return nil, &RequestError{RequestID: requestID, Err: err}
	}
	result := &Result{RequestID: requestID, SelectedDeployment: request.SelectedDeploymentID, Cost: request.ProviderCost, LatencyMS: request.Latency.Milliseconds()}
	if request.Usage != nil {
		result.Usage = *request.Usage
	}
	if request.Response != nil && len(request.Response.Choices) > 0 {
		result.Output = request.Response.Choices[0].Message.Content
	}
	return result, nil
}
