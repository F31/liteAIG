// Package admission performs cheap trusted-scope checks before the pipeline hot path.
package admission

import (
	"context"
	"errors"
	"github.com/F31/liteAIG/internal/access/auth"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	kernelerrors "github.com/F31/liteAIG/internal/kernel/errors"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

type Authenticator interface {
	Authenticate(context.Context, string, auth.RequestScope) (*auth.Result, error)
}
type Config struct{ MaxBodyBytes int64 }
type Input struct {
	Token, RemoteIP, RequestedTenantID, RequestedProjectID, Protocol string
	BodyBytes                                                        int64
	Request                                                          *interaction.UnifiedRequest
}
type Service struct {
	config Config
	auth   Authenticator
	ids    contracts.IDGenerator
	clock  contracts.Clock
}

func New(config Config, authenticator Authenticator, ids contracts.IDGenerator, clock contracts.Clock) (*Service, error) {
	if config.MaxBodyBytes <= 0 || authenticator == nil || ids == nil || clock == nil {
		return nil, errors.New("complete admission configuration is required")
	}
	return &Service{config: config, auth: authenticator, ids: ids, clock: clock}, nil
}
func (s *Service) Admit(ctx context.Context, input Input) (*kernel.RequestContext, error) {
	requestID, err := s.ids.New()
	if err != nil {
		return nil, err
	}
	fail := func(code, message string) error {
		return &kernelerrors.Error{Code: code, Message: message, RequestID: requestID}
	}
	if input.BodyBytes < 0 || input.BodyBytes > s.config.MaxBodyBytes {
		return nil, fail("REQUEST_TOO_LARGE", "request body exceeds configured limit")
	}
	if input.Request == nil || input.Request.Model == "" {
		return nil, fail("INVALID_REQUEST", "model request is required")
	}
	authenticated, err := s.auth.Authenticate(ctx, input.Token, auth.RequestScope{TenantID: input.RequestedTenantID, ProjectID: input.RequestedProjectID, RemoteIP: input.RemoteIP})
	if err != nil {
		return nil, fail("UNAUTHORIZED", "invalid credentials")
	}
	if !modelAllowed(authenticated.Key.ModelAllowlist, input.Request.Model) {
		return nil, fail("MODEL_FORBIDDEN", "model is not allowed")
	}
	principalID := authenticated.Principal.APIKeyID
	switch authenticated.Principal.Type {
	case "agent":
		principalID = authenticated.Principal.AgentID
	case "application":
		principalID = authenticated.Principal.ApplicationID
	case "service_account":
		principalID = authenticated.Principal.ServiceAccountID
	}
	interactionContext := &interaction.Context{Kind: interaction.KindModel, Protocol: input.Protocol, TenantID: authenticated.Principal.TenantID, ProjectID: authenticated.Principal.ProjectID, SessionID: input.Request.SessionID, TaskID: input.Request.TaskID, RootTaskID: input.Request.RootTaskID, ParentTaskID: input.Request.ParentTaskID, Caller: interaction.PrincipalRef{Type: authenticated.Principal.Type, ID: principalID}, Target: interaction.ResourceRef{Type: "logical_model", ID: input.Request.Model}}
	return &kernel.RequestContext{RequestID: requestID, ReceivedAt: s.clock.Now(), Snapshot: authenticated.Snapshot, Interaction: interactionContext, Request: input.Request, Key: authenticated.Key}, nil
}
func modelAllowed(values []string, model string) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if value == model {
			return true
		}
	}
	return false
}
