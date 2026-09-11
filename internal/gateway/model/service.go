// Package model coordinates route resolution and connector execution.
package model

import (
	"context"
	"github.com/F31/liteAIG/internal/gateway/execution"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	routing "github.com/F31/liteAIG/internal/routing/model"
)

type Service struct {
	planner  *routing.Planner
	executor *execution.Executor
}

func New(planner *routing.Planner, executor *execution.Executor) *Service {
	return &Service{planner: planner, executor: executor}
}

type Input struct {
	Snapshot                         *runtime.TenantRuntimeSnapshot
	Key                              runtime.APIKey
	RequestID, ProjectID, RoutingKey string
	RequiredCapabilities             []string
	ContextTokens                    int
	Health                           map[string]bool
	Circuit                          routing.CircuitView
	Request                          *interaction.UnifiedRequest
}

func (s *Service) Invoke(ctx context.Context, input Input) (*routing.RoutePlan, *execution.Result, error) {
	plan, err := s.planner.Plan(routing.Input{Snapshot: input.Snapshot, Key: input.Key, LogicalModel: input.Request.Model, ProjectID: input.ProjectID, RequestID: input.RequestID, RoutingKey: input.RoutingKey, RequiredCapabilities: input.RequiredCapabilities, ContextTokens: input.ContextTokens, Health: input.Health, Circuit: input.Circuit})
	if err != nil {
		return plan, nil, err
	}
	result, err := s.executor.Execute(ctx, plan, input.Snapshot, input.Request)
	return plan, result, err
}
func (s *Service) Stream(ctx context.Context, input Input, writer contracts.StreamWriter) (*routing.RoutePlan, []execution.Attempt, error) {
	plan, err := s.planner.Plan(routing.Input{Snapshot: input.Snapshot, Key: input.Key, LogicalModel: input.Request.Model, ProjectID: input.ProjectID, RequestID: input.RequestID, RoutingKey: input.RoutingKey, RequiredCapabilities: input.RequiredCapabilities, ContextTokens: input.ContextTokens, Health: input.Health, Circuit: input.Circuit})
	if err != nil {
		return plan, nil, err
	}
	attempts, err := s.executor.ExecuteStream(ctx, plan, input.Snapshot, input.Request, writer)
	return plan, attempts, err
}
