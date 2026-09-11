package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/access/auth"
	"github.com/F31/liteAIG/internal/access/protocol/openai"
	agentrouting "github.com/F31/liteAIG/internal/agentic/routing"
	a2aconnector "github.com/F31/liteAIG/internal/connectors/agent/a2a"
	mcpconnector "github.com/F31/liteAIG/internal/connectors/tool/mcp"
	"github.com/F31/liteAIG/internal/federation"
	"github.com/F31/liteAIG/internal/gateway/admission"
	gatewayserver "github.com/F31/liteAIG/internal/gateway/server"
	toolgate "github.com/F31/liteAIG/internal/gateway/tool"
	"github.com/F31/liteAIG/internal/identity"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	kernelerrors "github.com/F31/liteAIG/internal/kernel/errors"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/platform/egress"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/internal/tenancy"
)

// gatewayMaxBodyBytes bounds inbound data-plane request bodies.
const gatewayMaxBodyBytes = 1 << 20

// liteGatewayCore implements the data-plane governance surface for the gateway
// HTTP server: bearer-token authentication, admission, the governed pipeline,
// and model visibility.
type liteGatewayCore struct {
	admission     *admission.Service
	pipeline      *litePipeline
	authenticator *auth.APIKeyAuthenticator
	http          *http.Client
	toolCalls     *sqlrepo.ToolCallStore
	a2aTasks      *sqlrepo.A2ATaskStore
	connectors    *connectorCache
	secrets       a2aconnector.SecretResolver

	// inboundFederated is the pluggable federated-identity seam. When wired,
	// an inbound /a2a request that carries trusted-proxy federated transport
	// facts and no bearer token is resolved to a federated principal instead
	// of an API key.
	inboundFederated *inboundFederatedAuth
}

// inboundFederatedAuth resolves federated inbound identity: it lists the
// tenants a federated caller may be scoped to and fetches the compiled runtime
// snapshot for a resolved tenant.
type inboundFederatedAuth struct {
	resolver *federation.Resolver
	// tenants lists the candidate tenant scopes for a federated caller.
	tenants func(context.Context) ([]tenancy.TenantScope, error)
	// snapshotFor returns the compiled runtime snapshot for a tenant id.
	snapshotFor func(context.Context, string) (*runtime.TenantRuntimeSnapshot, error)
}

func newLiteGatewayCore(admissionService *admission.Service, pipeline *litePipeline, authenticator *auth.APIKeyAuthenticator, toolCalls *sqlrepo.ToolCallStore, a2aTasks *sqlrepo.A2ATaskStore, httpClient *http.Client, secrets a2aconnector.SecretResolver, federated *inboundFederatedAuth) *liteGatewayCore {
	return &liteGatewayCore{admission: admissionService, pipeline: pipeline, authenticator: authenticator, http: httpClient, toolCalls: toolCalls, a2aTasks: a2aTasks, connectors: newConnectorCache(), secrets: secrets, inboundFederated: federated}
}

// connectorCache reuses the stateless MCP/A2A connectors per base URL so each
// request does not rebuild one.
type connectorCache struct {
	mu  sync.Mutex
	mcp map[string]*mcpconnector.Connector
	a2a map[string]*a2aconnector.Connector
}

func newConnectorCache() *connectorCache {
	return &connectorCache{mcp: map[string]*mcpconnector.Connector{}, a2a: map[string]*a2aconnector.Connector{}}
}

func (c *connectorCache) mcpFor(url string, client *http.Client) (*mcpconnector.Connector, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.mcp[url]; ok {
		return cached, nil
	}
	connector, err := mcpconnector.New(mcpconnector.Config{BaseURL: url, Timeout: 30 * time.Second}, client, nil)
	if err != nil {
		return nil, err
	}
	c.mcp[url] = connector
	return connector, nil
}

func (c *connectorCache) a2aFor(config a2aconnector.Config, client *http.Client, secrets a2aconnector.SecretResolver) (*a2aconnector.Connector, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Key on URL + SecretRef so two endpoints that share a URL but use
	// different credentials never share a cached connector.
	key := config.BaseURL + "\x00" + config.SecretRef
	if cached, ok := c.a2a[key]; ok {
		return cached, nil
	}
	connector, err := a2aconnector.New(config, client, secrets)
	if err != nil {
		return nil, err
	}
	c.a2a[key] = connector
	return connector, nil
}

func (c *liteGatewayCore) Admit(ctx context.Context, input gatewayserver.AdmitInput) (*kernel.RequestContext, error) {
	request, err := c.admission.Admit(ctx, admission.Input{
		Token:     input.Token,
		RemoteIP:  input.RemoteIP,
		Protocol:  input.Protocol,
		BodyBytes: input.BodyBytes,
		Request:   input.Request,
	})
	if err != nil {
		return nil, err
	}
	request.Source = "gateway"
	return request, nil
}

func (c *liteGatewayCore) Run(ctx context.Context, request *kernel.RequestContext, writer contracts.StreamWriter) error {
	if writer != nil {
		request.StreamWriter = writer
	}
	err := c.pipeline.Run(ctx, request)
	if err == nil {
		c.recordChatToolCalls(ctx, request)
	}
	return err
}

// recordChatToolCalls persists model-generated function calls from chat
// responses to the tool call ledger (Request Explorer). Recording is
// best-effort: a ledger failure never fails the request.
func (c *liteGatewayCore) recordChatToolCalls(ctx context.Context, request *kernel.RequestContext) {
	if c.toolCalls == nil || request == nil || request.Response == nil || request.Request == nil || request.Request.Chat == nil {
		return
	}
	now := c.pipeline.clock.Now()
	for _, choice := range request.Response.Choices {
		for _, call := range choice.Message.ToolCalls {
			name := call.Function.Name
			if name == "" {
				continue
			}
			id, idErr := c.pipeline.ids.New()
			if idErr != nil {
				id = request.RequestID + ".tool." + name
			}
			event := sqlrepo.ToolCallEvent{ID: id, TenantID: request.TenantID(), ProjectID: request.ProjectID(), RequestID: request.RequestID, SessionID: request.Request.SessionID, TaskID: request.Request.TaskID, ToolID: name, ToolName: name, OccurredAt: now}
			if request.Interaction != nil && request.Interaction.Caller.Type == "agent" {
				event.AgentID = request.Interaction.Caller.ID
			}
			_ = c.toolCalls.Create(ctx, tenancy.TenantScope{TenantID: request.TenantID()}, event)
		}
	}
}

func (c *liteGatewayCore) Models(ctx context.Context, token, remoteIP string) (*openai.ModelsResponse, error) {
	result, err := c.authenticator.Authenticate(ctx, token, auth.RequestScope{RemoteIP: remoteIP})
	if err != nil {
		return nil, &kernelerrors.Error{Code: "UNAUTHORIZED", Message: "invalid credentials"}
	}
	list := openai.ListModels(result.Snapshot, result.Key)
	return &list, nil
}

func (c *liteGatewayCore) Tool(ctx context.Context, input gatewayserver.AdmitInput) (*interaction.UnifiedResponse, error) {
	// Inbound federated identity: the server only stamps input.Federated when
	// the request carried no usable bearer token, so a non-nil Federated with
	// an empty token takes the federated path instead of failing API-key auth.
	if input.Federated != nil && input.Token == "" {
		return c.toolFederated(ctx, input)
	}
	return c.tool(ctx, input)
}

// ToolStream satisfies the gateway server's optional ToolStreamer surface used
// by the inbound A2A streaming profile. For A2A requests it runs the same
// governed, durable, fully-accounted relay as Tool but over the outbound
// connector's real incremental SSE consumption (see connectors/agent/a2a.Stream),
// forwarding each remote text delta to the writer: one message event per delta
// followed by a completed event. Any other protocol keeps the historical
// behavior: the blocking Tool result is rendered as a single delta + final.
func (c *liteGatewayCore) ToolStream(ctx context.Context, input gatewayserver.AdmitInput, writer contracts.StreamWriter) error {
	if input.Protocol == "a2a" {
		return c.toolStreamA2A(ctx, input, writer)
	}
	response, err := c.Tool(ctx, input)
	if err != nil {
		return err
	}
	content := ""
	id := ""
	if response != nil {
		id = response.ID
		if response.ToolResult != nil {
			content = response.ToolResult.Content
		}
	}
	if content != "" {
		if err := writer.WriteChunk(ctx, contracts.StreamChunk{Event: interaction.StreamEvent{ID: id, Delta: content}}); err != nil {
			return err
		}
	}
	return writer.WriteChunk(ctx, contracts.StreamChunk{Event: interaction.StreamEvent{ID: id, Final: true}})
}

// toolStreamA2A resolves the caller the same way Tool does (federated transport
// facts or a bearer API key) and relays the A2A request to the outbound agent
// incrementally, forwarding deltas to the writer. Errors map to the same codes
// as the blocking path so the SSE surface behaves consistently.
func (c *liteGatewayCore) toolStreamA2A(ctx context.Context, input gatewayserver.AdmitInput, writer contracts.StreamWriter) error {
	var authResult *auth.Result
	var err error
	if input.Federated != nil && input.Token == "" {
		authResult, err = c.federatedAuthResult(ctx, input)
	} else {
		authResult, err = c.authenticator.Authenticate(ctx, input.Token, auth.RequestScope{RemoteIP: input.RemoteIP})
		if err == nil && (input.BodyBytes < 0 || input.BodyBytes > gatewayMaxBodyBytes) {
			err = &kernelerrors.Error{Code: "REQUEST_TOO_LARGE", Message: "request body exceeds configured limit"}
		}
		if err != nil {
			return &kernelerrors.Error{Code: "UNAUTHORIZED", Message: "invalid credentials"}
		}
	}
	if err != nil {
		return err
	}
	_, err = c.invokeA2AStream(ctx, authResult, input, writer)
	return err
}

// tool is the shared bearer-authenticated tool/agent invocation body behind
// Tool and the federated path's endpoint call.
func (c *liteGatewayCore) tool(ctx context.Context, input gatewayserver.AdmitInput) (*interaction.UnifiedResponse, error) {
	result, err := c.authenticator.Authenticate(ctx, input.Token, auth.RequestScope{RemoteIP: input.RemoteIP})
	if err != nil {
		return nil, &kernelerrors.Error{Code: "UNAUTHORIZED", Message: "invalid credentials"}
	}
	if input.BodyBytes < 0 || input.BodyBytes > gatewayMaxBodyBytes {
		return nil, &kernelerrors.Error{Code: "REQUEST_TOO_LARGE", Message: "request body exceeds configured limit"}
	}
	if input.Protocol == "a2a" {
		return c.invokeA2A(ctx, result, input)
	}
	if input.Request == nil || input.Request.Tool == nil {
		return nil, &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "tool request is required"}
	}
	if input.Request.Tool.Name == "server/discover" {
		return c.discoverTools(result.Snapshot)
	}
	tool, ok := toolByName(result.Snapshot, input.Request.Tool.Name)
	if !ok {
		return nil, &kernelerrors.Error{Code: "TOOL_NOT_FOUND", Message: "tool not available"}
	}
	requestID, err := c.pipeline.ids.New()
	if err != nil {
		return nil, err
	}
	req := &kernel.RequestContext{
		RequestID:  requestID,
		ReceivedAt: c.pipeline.clock.Now(),
		Snapshot:   result.Snapshot,
		Interaction: &interaction.Context{
			Kind:      interaction.KindTool,
			Protocol:  input.Protocol,
			TenantID:  result.Principal.TenantID,
			ProjectID: result.Principal.ProjectID,
			Caller:    interaction.PrincipalRef{Type: result.Principal.Type, ID: principalID(result.Principal)},
			Target:    interaction.ResourceRef{Type: "tool", ID: tool.ID},
		},
		Request: input.Request,
		Source:  "gateway",
		Key:     result.Key,
	}
	if _, err := (toolgate.Handler{}).Handle(ctx, req); err != nil {
		return nil, err
	}
	if _, err := c.pipeline.approvals.Handle(ctx, req); err != nil {
		_ = c.pipeline.finalizeTool(ctx, req, tool.ID, nil, err)
		return nil, err
	}
	server, ok := mcpServer(result.Snapshot, tool.ServerID)
	if !ok || server.URL == "" || server.Status != "active" {
		return nil, &kernelerrors.Error{Code: "TOOL_NOT_FOUND", Message: "tool server not available", RequestID: requestID}
	}
	if err := egress.ValidateTarget(server.URL, egress.LitePolicy()); err != nil {
		return nil, &kernelerrors.Error{Code: "UPSTREAM_INVALID", Message: err.Error(), RequestID: requestID}
	}
	if err := c.pipeline.admitTool(req); err != nil {
		_ = c.pipeline.finalizeTool(ctx, req, tool.ID, nil, err)
		return nil, err
	}
	connector, err := c.connectors.mcpFor(server.URL, c.http)
	if err != nil {
		_ = c.pipeline.finalizeTool(ctx, req, tool.ID, nil, err)
		return nil, err
	}
	invocation := contracts.InvocationRequest{Target: contracts.TargetRef{Kind: "tool", ID: tool.ID}, Request: input.Request, TenantID: result.Principal.TenantID, ProjectID: result.Principal.ProjectID, RequestID: requestID}
	response, err := c.invokeWithResilience(ctx, requestID, "mcp-server:"+server.ID, "", func() (*interaction.UnifiedResponse, error) {
		invoked, invokeErr := connector.Invoke(ctx, invocation)
		if invokeErr != nil {
			return nil, invokeErr
		}
		return invoked.Response, nil
	})
	if err != nil {
		_ = c.pipeline.finalizeTool(ctx, req, tool.ID, nil, err)
		return nil, connector.NormalizeError(err)
	}
	if c.toolCalls != nil {
		if err := c.toolCalls.Create(ctx, tenancy.TenantScope{TenantID: result.Principal.TenantID}, sqlrepo.ToolCallEvent{ID: requestID + ".tool", TenantID: result.Principal.TenantID, ProjectID: result.Principal.ProjectID, RequestID: requestID, AgentID: result.Principal.AgentID, ToolID: tool.ID, ToolName: tool.Name, OccurredAt: c.pipeline.clock.Now()}); err != nil {
			_ = c.pipeline.finalizeTool(ctx, req, tool.ID, response, nil)
			return nil, err
		}
	}
	if err := c.pipeline.finalizeTool(ctx, req, tool.ID, response, nil); err != nil {
		return nil, err
	}
	return response, nil
}

// toolFederated resolves an inbound request whose transport facts (set by a
// trusted proxy, not the remote peer) identify a federated principal. Only the
// A2A relay path accepts a federated caller today; MCP/model tool requests with
// federated facts are refused so an untrusted inbound identity never reaches an
// internal tool.
func (c *liteGatewayCore) toolFederated(ctx context.Context, input gatewayserver.AdmitInput) (*interaction.UnifiedResponse, error) {
	authResult, err := c.federatedAuthResult(ctx, input)
	if err != nil {
		return nil, err
	}
	return c.invokeA2A(ctx, authResult, input)
}

// federatedAuthResult resolves an inbound federated principal from trusted
// transport facts. Errors map to the same codes the blocking relay surfaces so
// a streaming caller observes identical failures.
func (c *liteGatewayCore) federatedAuthResult(ctx context.Context, input gatewayserver.AdmitInput) (*auth.Result, error) {
	if c.inboundFederated == nil || c.inboundFederated.resolver == nil || c.inboundFederated.tenants == nil || c.inboundFederated.snapshotFor == nil {
		return nil, &kernelerrors.Error{Code: "FEDERATION_UNTRUSTED", Message: "inbound federated identity is not configured"}
	}
	if input.Protocol != "a2a" {
		return nil, &kernelerrors.Error{Code: "UNAUTHORIZED", Message: "federated transport is not accepted on this path"}
	}
	if input.BodyBytes < 0 || input.BodyBytes > gatewayMaxBodyBytes {
		return nil, &kernelerrors.Error{Code: "REQUEST_TOO_LARGE", Message: "request body exceeds configured limit"}
	}
	scopes, err := c.inboundFederated.tenants(ctx)
	if err != nil || len(scopes) == 0 {
		return nil, &kernelerrors.Error{Code: "NOT_FOUND", Message: "no tenant is available for federated identity"}
	}
	if len(scopes) > 1 {
		return nil, &kernelerrors.Error{Code: "NOT_FOUND", Message: "ambiguous tenant"}
	}
	scope := scopes[0]
	authMethod := canonicalFederatedAuthMethod(input.Federated.AuthMethod)
	if authMethod == "" {
		return nil, &kernelerrors.Error{Code: "UNAUTHORIZED", Message: "invalid federated transport auth method"}
	}
	principal, err := c.inboundFederated.resolver.Resolve(ctx, scope, federation.TransportFacts{
		AuthMethod: authMethod,
		Subject:    input.Federated.Subject,
		Issuer:     input.Federated.Issuer,
	})
	if err != nil {
		return nil, &kernelerrors.Error{Code: "FEDERATION_UNTRUSTED", Message: "untrusted federated identity"}
	}
	snapshot, err := c.inboundFederated.snapshotFor(ctx, scope.TenantID)
	if err != nil || snapshot == nil {
		return nil, &kernelerrors.Error{Code: "NOT_FOUND", Message: "tenant snapshot is unavailable for federated identity"}
	}
	return &auth.Result{Principal: *principal, Snapshot: snapshot, Key: runtime.APIKey{}}, nil
}

// canonicalFederatedAuthMethod maps the trusted-proxy header vocabulary onto
// the resolver's canonical federated auth methods, accepting both the anchor
// type spellings (mtls_spki/oidc/jws/registry_attestation) and the transport
// spellings (federated_mtls/federated_oidc/federated_jws/registry_attested).
func canonicalFederatedAuthMethod(value string) string {
	switch value {
	case "mtls_spki", "federated_mtls":
		return identity.AuthMethodFederatedMTLS
	case "oidc", "federated_oidc":
		return identity.AuthMethodFederatedOIDC
	case "jws", "federated_jws":
		return identity.AuthMethodFederatedJWS
	case "registry_attestation", "registry_attested":
		return identity.AuthMethodRegistryAttested
	default:
		return ""
	}
}

func (c *liteGatewayCore) invokeA2A(ctx context.Context, authResult *auth.Result, input gatewayserver.AdmitInput) (*interaction.UnifiedResponse, error) {
	return c.invokeA2ARelay(ctx, authResult, input, nil)
}

// invokeA2AStream is the streaming relay: the caller identity is already
// resolved and every outbound delta is forwarded to writer through the same
// governed, durable, fully-accounted admission as the blocking relay.
func (c *liteGatewayCore) invokeA2AStream(ctx context.Context, authResult *auth.Result, input gatewayserver.AdmitInput, writer contracts.StreamWriter) (*interaction.UnifiedResponse, error) {
	return c.invokeA2ARelay(ctx, authResult, input, writer)
}

// invokeA2ARelay runs one inbound A2A message through the outbound
// message/send relay. When sink is nil the blocking connector Invoke is used
// (with the circuit breaker and bounded retry); when sink is non-nil the
// connector's true incremental SSE consumption drives it and each delta is
// forwarded to the sink before the stream completes. Both paths share the same
// admission, durable task/idempotency handling, and accounting.
func (c *liteGatewayCore) invokeA2ARelay(ctx context.Context, authResult *auth.Result, input gatewayserver.AdmitInput, sink contracts.StreamWriter) (*interaction.UnifiedResponse, error) {
	if input.Request == nil || input.Request.Chat == nil {
		return nil, &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "A2A chat message is required"}
	}
	now := c.pipeline.clock.Now()
	plan, err := agentrouting.NewRouter(nil).Plan(authResult.Snapshot, "chat", map[string]agentrouting.EndpointMetrics{}, [2]time.Time{now, now})
	if err != nil || plan.Selected == "" {
		return nil, &kernelerrors.Error{Code: "NOT_FOUND", Message: "A2A agent endpoint is not configured"}
	}
	endpoint, ok := authResult.Snapshot.AgentEndpoint(plan.Selected)
	if !ok || endpoint.Protocol != "a2a" || endpoint.URL == "" {
		return nil, &kernelerrors.Error{Code: "NOT_FOUND", Message: "A2A agent endpoint is not configured"}
	}
	if err := egress.ValidateTarget(endpoint.URL, egress.LitePolicy()); err != nil {
		return nil, &kernelerrors.Error{Code: "UPSTREAM_INVALID", Message: err.Error()}
	}
	requestID, err := c.pipeline.ids.New()
	if err != nil {
		return nil, err
	}
	// A federated caller carries no internal project scope (its principal is
	// external). Accounting requires a real project row, so the relay is
	// attributed to the project that owns the outbound agent endpoint; the
	// inbound caller's external identity is preserved separately on the
	// interaction.
	projectID := authResult.Principal.ProjectID
	if authResult.Principal.TrustBoundary == identity.BoundaryExternalFederated && projectID == "" {
		if agent, agentOK := authResult.Snapshot.Agent(endpoint.AgentID); agentOK && agent.ProjectID != "" {
			projectID = agent.ProjectID
		}
	}
	req := &kernel.RequestContext{
		RequestID:  requestID,
		ReceivedAt: now,
		Snapshot:   authResult.Snapshot,
		Interaction: &interaction.Context{
			Kind:      interaction.KindAgent,
			Protocol:  "a2a",
			TenantID:  authResult.Principal.TenantID,
			ProjectID: projectID,
			Caller:    interaction.PrincipalRef{Type: authResult.Principal.Type, ID: principalID(authResult.Principal)},
			Target:    interaction.ResourceRef{Type: "agent", ID: endpoint.AgentID},
		},
		Request: input.Request,
		Source:  "gateway",
		Key:     authResult.Key,
	}
	if err := enforceA2AFederation(req, endpoint, "chat"); err != nil {
		_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, err)
		return nil, err
	}
	// When the caller is an inbound federated principal and the relay target is
	// local (enforceA2AFederation found no outbound boundary to stamp), record
	// the inbound boundary on the interaction so accounting/telemetry see the
	// external caller. The caller itself is already preserved on the context.
	if authResult.Principal.TrustBoundary == identity.BoundaryExternalFederated && !isExternalFederatedAgent(req.Snapshot, endpoint.AgentID) {
		req.Interaction.Direction = interaction.DirectionInbound
		req.Interaction.TrustBoundary = interaction.TrustBoundaryExternalFederated
		if req.Interaction.Federation == nil {
			req.Interaction.Federation = &interaction.FederationContext{}
		}
		req.Interaction.Federation.RelationshipID = authResult.Principal.FederationRelationshipID
		req.Interaction.Federation.ExternalAgentID = authResult.Principal.AgentID
	}
	// Durable outbound-relay task state (Work Package 4). The relay is
	// deduplicated by an idempotency key (explicit metadata, else the A2A
	// message id, else the Lite request id): a completed task replays its
	// stored result, an in-flight one is refused with A2A_TASK_IN_PROGRESS,
	// and a failed one may be retried against the same durable task. The task
	// row itself is keyed by the Lite-generated request id.
	//
	// Dedupe deliberately runs AFTER enforceA2AFederation: a revoked or
	// suspended relationship still blocks a replayed task (fail closed), and
	// the inbound delegation-hop bound still applies before any stored reply.
	idempotencyKey := a2aIdempotencyKey(req.Request, requestID)
	tenantScope := tenancy.TenantScope{TenantID: authResult.Principal.TenantID}
	staleBefore := now.Add(-a2aTaskRunningTTL)
	var durableTask *sqlrepo.A2ATask
	if c.a2aTasks != nil {
		if _, err := c.a2aTasks.ReapStaleRunning(ctx, tenantScope, staleBefore); err != nil {
			_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, err)
			return nil, err
		}
		if existing, ok, err := c.a2aTasks.GetByIdempotency(ctx, tenantScope, idempotencyKey); err != nil {
			_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, err)
			return nil, err
		} else if ok {
			switch existing.Status {
			case sqlrepo.A2ATaskCompleted:
				// Idempotent replay: an identical stored body, no connector call
				// and no counter advance. Admission is skipped because this work
				// was already accepted and paid for.
				req.Interaction.TaskID = existing.TaskID
				return a2aReplayResponse(endpoint.AgentID, existing), nil
			case sqlrepo.A2ATaskPending:
				err := &kernelerrors.Error{Code: "A2A_TASK_IN_PROGRESS", Message: "a task for this idempotency key is already in progress", RequestID: requestID}
				_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, err)
				return nil, err
			case sqlrepo.A2ATaskRunning:
				err := &kernelerrors.Error{Code: "A2A_TASK_IN_PROGRESS", Message: "a task for this idempotency key is already in progress", RequestID: requestID}
				_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, err)
				return nil, err
			default:
				// failed (or cancelled): a retry proceeds against the same task.
				durableTask = &existing
			}
		}
	}
	if err := c.pipeline.admitTool(req); err != nil {
		_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, err)
		return nil, err
	}
	if _, err := c.pipeline.approvals.Handle(ctx, req); err != nil {
		_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, err)
		return nil, err
	}
	// Durable admission happens after the approval/admission stages so a
	// rejected or rate-limited request never leaves a stuck pending row.
	if c.a2aTasks != nil {
		if durableTask == nil {
			createErr := c.a2aTasks.Create(ctx, tenantScope, sqlrepo.A2ATask{
				TaskID: requestID, TenantID: authResult.Principal.TenantID,
				ExternalAgentID: endpoint.AgentID, ProjectID: projectID,
				RequestID: requestID, IdempotencyKey: idempotencyKey,
				Status: sqlrepo.A2ATaskPending, Message: a2aRelayText(req.Request),
			})
			if createErr != nil {
				_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, createErr)
				return nil, createErr
			}
			// A concurrent identical request may have won the insert and own the
			// key; adopt its durable task (or its finished replay).
			var ok bool
			var created sqlrepo.A2ATask
			if created, ok, err = c.a2aTasks.GetByTask(ctx, tenantScope, requestID); err != nil {
				_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, err)
				return nil, err
			} else if ok {
				durableTask = &created
			} else {
				if other, otherOK, getErr := c.a2aTasks.GetByIdempotency(ctx, tenantScope, idempotencyKey); getErr != nil {
					_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, getErr)
					return nil, getErr
				} else if otherOK {
					switch other.Status {
					case sqlrepo.A2ATaskCompleted:
						req.Interaction.TaskID = other.TaskID
						return a2aReplayResponse(endpoint.AgentID, other), nil
					case sqlrepo.A2ATaskPending, sqlrepo.A2ATaskRunning:
						err := &kernelerrors.Error{Code: "A2A_TASK_IN_PROGRESS", Message: "a task for this idempotency key is already in progress", RequestID: requestID}
						_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, err)
						return nil, err
					default:
						durableTask = &other
					}
				} else {
					err := errors.New("durable A2A task disappeared after create")
					_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, err)
					return nil, err
				}
			}
		}
		started, startErr := c.a2aTasks.TryStart(ctx, tenantScope, durableTask.TaskID, staleBefore)
		if startErr != nil {
			_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, startErr)
			return nil, startErr
		}
		if !started {
			err := &kernelerrors.Error{Code: "A2A_TASK_IN_PROGRESS", Message: "a task for this idempotency key is already in progress", RequestID: requestID}
			_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, err)
			return nil, err
		}
		// Consume the durable hop/call budget for this relay attempt.
		hops := a2aHopCount(req.Request) + 1
		applied, advanceErr := c.a2aTasks.AdvanceCounters(ctx, tenantScope, durableTask.TaskID, hops, 1, maxA2ADelegationHops, maxA2ATaskCalls)
		if advanceErr != nil {
			_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, advanceErr)
			return nil, advanceErr
		}
		if !applied {
			limitErr := a2aTaskLimitError(ctx, c.a2aTasks, tenantScope, durableTask.TaskID, hops, requestID)
			_ = c.a2aTasks.SetStatusResult(ctx, tenantScope, durableTask.TaskID, sqlrepo.A2ATaskFailed, "")
			_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, limitErr)
			return nil, limitErr
		}
		req.Interaction.TaskID = durableTask.TaskID
	}
	connector, err := c.connectors.a2aFor(a2aconnector.Config{
		BaseURL: endpoint.URL, Timeout: 30 * time.Second,
		SecretRef: endpoint.SecretRef, AuthScheme: endpoint.AuthScheme, Headers: endpoint.Headers,
	}, c.http, c.secrets)
	if err != nil {
		_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, err)
		return nil, err
	}
	invocation := contracts.InvocationRequest{Target: contracts.TargetRef{Kind: "agent", ID: endpoint.AgentID}, Request: input.Request, TenantID: authResult.Principal.TenantID, ProjectID: projectID, RequestID: requestID, AgentID: endpoint.AgentID, CredentialID: endpoint.SecretRef}
	var response *interaction.UnifiedResponse
	var invokeErr error
	if sink == nil {
		// Blocking relay: circuit-breaker guarded with a bounded retry, as before.
		response, invokeErr = c.invokeWithResilience(ctx, requestID, "a2a-agent:"+endpoint.AgentID, endpoint.SecretRef, func() (*interaction.UnifiedResponse, error) {
			invoked, invokeErr := connector.Invoke(ctx, invocation)
			if invokeErr != nil {
				return nil, invokeErr
			}
			return invoked.Response, nil
		})
	} else {
		// Streaming relay: request the LiteAIG A2A SSE profile from the peer and
		// forward every incremental delta to the inbound writer before completing.
		// Remote A2A streams are never retried (message/send is not idempotent).
		streamRequest := *input.Request
		streamRequest.Stream = true
		invocation.Request = &streamRequest
		forwarder := &relayDeltaForwarder{id: requestID, agentID: endpoint.AgentID, sink: sink}
		invokeErr = connector.Stream(ctx, invocation, forwarder)
		if invokeErr == nil {
			response = forwarder.response()
		}
	}
	if invokeErr != nil {
		if c.a2aTasks != nil && durableTask != nil {
			_ = c.a2aTasks.SetStatusResult(ctx, tenantScope, durableTask.TaskID, sqlrepo.A2ATaskFailed, "")
		}
		_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, nil, invokeErr)
		return nil, connector.NormalizeError(invokeErr)
	}
	if c.a2aTasks != nil && durableTask != nil {
		content := ""
		if response != nil && response.ToolResult != nil {
			content = response.ToolResult.Content
		}
		if response != nil {
			response.ID = durableTask.TaskID
		}
		if err := c.a2aTasks.SetStatusResult(ctx, tenantScope, durableTask.TaskID, sqlrepo.A2ATaskCompleted, content); err != nil {
			_ = c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, response, err)
			return nil, err
		}
	}
	if err := c.pipeline.finalizeTool(ctx, req, endpoint.AgentID, response, nil); err != nil {
		return nil, err
	}
	return response, nil
}

// a2aReplayResponse rebuilds the stored completion of a durable A2A task so an
// idempotent retry returns an identical body without re-hitting the relay.
func a2aReplayResponse(agentID string, task sqlrepo.A2ATask) *interaction.UnifiedResponse {
	return &interaction.UnifiedResponse{ID: task.TaskID, ToolResult: &interaction.ToolResult{
		Name:       agentID,
		Content:    task.Result,
		Provenance: interaction.Provenance{Source: "a2a.task_replay"},
	}}
}

// relayDeltaForwarder forwards incremental outbound A2A deltas to the inbound
// SSE writer while accumulating the completed reply for accounting and durable
// task persistence. The remote [completed] frame signs the final chunk, which
// the SSE writer turns into the terminal completed event.
type relayDeltaForwarder struct {
	id      string
	agentID string
	sink    contracts.StreamWriter
	content string
}

func (d *relayDeltaForwarder) WriteChunk(ctx context.Context, chunk contracts.StreamChunk) error {
	if chunk.Event.Delta != "" {
		d.content += chunk.Event.Delta
	}
	if d.sink == nil {
		return nil
	}
	return d.sink.WriteChunk(ctx, contracts.StreamChunk{Event: interaction.StreamEvent{ID: d.id, Delta: chunk.Event.Delta, Final: chunk.Event.Final}})
}

func (d *relayDeltaForwarder) response() *interaction.UnifiedResponse {
	return &interaction.UnifiedResponse{
		ID: d.id,
		ToolResult: &interaction.ToolResult{
			Name:       d.agentID,
			Content:    d.content,
			Provenance: interaction.Provenance{Source: "external_agent_response"},
		},
	}
}

// a2aIdempotencyKey derives the outbound-relay idempotency key: the explicit
// "a2a.idempotency_key" metadata when present, else the A2A message id, else
// the Lite-generated request id.
func a2aIdempotencyKey(request *interaction.UnifiedRequest, requestID string) string {
	if request != nil && request.Metadata != nil {
		if key := request.Metadata["a2a.idempotency_key"]; key != "" {
			return key
		}
	}
	if request != nil && request.SessionID != "" {
		return request.SessionID
	}
	return requestID
}

// a2aRelayText snapshots the outbound message text the gateway relays (the last
// non-empty chat turn), mirroring the A2A connector's chatTurn reduction.
func a2aRelayText(request *interaction.UnifiedRequest) string {
	if request == nil || request.Chat == nil {
		return ""
	}
	text := ""
	for _, message := range request.Chat.Messages {
		if message.Content != "" {
			text = message.Content
		}
	}
	return text
}

// a2aTaskLimitError reports which durable budget refused a relay admission: the
// delegation hop budget (FEDERATION_HOP_LIMIT) or the per-task call budget
// (A2A_TASK_LIMIT).
func a2aTaskLimitError(ctx context.Context, store *sqlrepo.A2ATaskStore, scope tenancy.TenantScope, taskID string, hops int, requestID string) error {
	if current, ok, err := store.GetByTask(ctx, scope, taskID); err == nil && ok && current.Hops+hops > maxA2ADelegationHops {
		return &kernelerrors.Error{Code: "FEDERATION_HOP_LIMIT", Message: "delegation hop limit exceeded", RequestID: requestID}
	}
	return &kernelerrors.Error{Code: "A2A_TASK_LIMIT", Message: "A2A task call limit exceeded", RequestID: requestID}
}

// invokeWithResilience runs a connector call under a per-target circuit
// breaker with one bounded retry for retryable upstream failures.
func (c *liteGatewayCore) invokeWithResilience(ctx context.Context, requestID, circuitKey, credentialKey string, invoke func() (*interaction.UnifiedResponse, error)) (*interaction.UnifiedResponse, error) {
	breaker := c.pipeline.circuit
	if !breaker.Allow(circuitKey, credentialKey) {
		return nil, &kernelerrors.Error{Code: "CIRCUIT_OPEN", Message: "upstream circuit is open", RequestID: requestID, Retryable: true}
	}
	response, err := invoke()
	if err != nil && ctx.Err() == nil && upstreamRetryable(err) {
		response, err = invoke()
	}
	breaker.Record(circuitKey, credentialKey, err == nil)
	return response, err
}

func upstreamRetryable(err error) bool {
	var upstream *contracts.UpstreamError
	return errors.As(err, &upstream) && upstream.Retryable
}

func targetName(request *interaction.UnifiedRequest, fallback string) string {
	if request == nil {
		return fallback
	}
	if request.Model != "" {
		return request.Model
	}
	if request.Tool != nil && request.Tool.Name != "" {
		return request.Tool.Name
	}
	return fallback
}

func (c *liteGatewayCore) discoverTools(snapshot *runtime.TenantRuntimeSnapshot) (*interaction.UnifiedResponse, error) {
	type toolWire struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	}
	tools := []toolWire{}
	for _, tool := range snapshot.Tools() {
		if tool.Status == "active" {
			tools = append(tools, toolWire{Name: tool.Name, InputSchema: tool.Schema})
		}
	}
	raw, err := json.Marshal(map[string]any{"tools": tools})
	if err != nil {
		return nil, err
	}
	return &interaction.UnifiedResponse{ToolResult: &interaction.ToolResult{Name: "server/discover", Content: string(raw), Provenance: interaction.Provenance{Source: "snapshot", Trusted: true}}}, nil
}

func toolByName(snapshot *runtime.TenantRuntimeSnapshot, name string) (runtime.Tool, bool) {
	for _, tool := range snapshot.Tools() {
		if tool.Name == name || tool.ID == name {
			return tool, true
		}
	}
	return runtime.Tool{}, false
}

func mcpServer(snapshot *runtime.TenantRuntimeSnapshot, id string) (runtime.MCPServer, bool) {
	for _, server := range snapshot.MCPServers() {
		if server.ID == id {
			return server, true
		}
	}
	return runtime.MCPServer{}, false
}

func principalID(principal identity.Principal) string {
	switch principal.Type {
	case "agent":
		return principal.AgentID
	case "application":
		return principal.ApplicationID
	case "service_account":
		return principal.ServiceAccountID
	default:
		return principal.APIKeyID
	}
}
