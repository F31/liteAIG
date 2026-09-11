// Package mcp invokes MCP 2026-07-28 Streamable HTTP tool servers.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	mcpprotocol "github.com/F31/liteAIG/internal/access/protocol/mcp"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

type Config struct {
	BaseURL string
	Timeout time.Duration
}

type Connector struct {
	config Config
	client *http.Client
	sink   contracts.EventSink
	now    func() time.Time
}

type DiscoveredTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

func New(config Config, client *http.Client, sink contracts.EventSink) (*Connector, error) {
	if config.BaseURL == "" || config.Timeout <= 0 || client == nil {
		return nil, errors.New("complete MCP connector configuration is required")
	}
	return &Connector{config: config, client: client, sink: sink, now: time.Now}, nil
}

func (c *Connector) Discover(ctx context.Context) ([]DiscoveredTool, error) {
	payload := []byte(`{"jsonrpc":"2.0","id":"discover","method":"server/discover","params":{}}`)
	callCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.config.BaseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	request.Header.Set("Mcp-Method", "server/discover")
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return nil, &contracts.UpstreamError{Code: "MCP_DISCOVERY_ERROR", StatusCode: response.StatusCode, Retryable: response.StatusCode >= 500, Message: "MCP discovery failed"}
	}
	var wire struct {
		Result struct {
			Tools []DiscoveredTool `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&wire); err != nil {
		return nil, err
	}
	return wire.Result.Tools, nil
}

func (c *Connector) Invoke(ctx context.Context, request contracts.InvocationRequest) (*contracts.InvocationResponse, error) {
	if request.Request == nil || request.Request.Tool == nil {
		return nil, errors.New("tool request is required")
	}
	method := mcpMethod(request.Request)
	params := any(map[string]any{"name": request.Request.Tool.Name, "arguments": request.Request.Tool.Arguments})
	if strings.HasPrefix(method, "tasks/") {
		params = request.Request.Tool.Arguments
	}
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.RequestID, "method": method, "params": params})
	callCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.config.BaseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	httpRequest.Header.Set("Mcp-Method", method)
	httpRequest.Header.Set("Mcp-Name", request.Request.Tool.Name)
	response, err := c.client.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return nil, &contracts.UpstreamError{Code: "MCP_UPSTREAM_ERROR", StatusCode: response.StatusCode, Retryable: response.StatusCode >= 500, Message: "MCP tool invocation failed"}
	}
	var wire struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&wire); err != nil {
		return nil, err
	}
	content := string(wire.Result)
	var toolResult struct {
		Content *string `json:"content"`
	}
	if json.Unmarshal(wire.Result, &toolResult) == nil && toolResult.Content != nil {
		content = *toolResult.Content
	}
	result := &interaction.ToolResult{Name: request.Request.Tool.Name, Content: content, Provenance: interaction.Provenance{Source: "tool_result", Trusted: false}}
	if c.sink != nil {
		_ = c.sink.Emit(ctx, contracts.DomainEvent{ID: request.RequestID + ".tool", Kind: "tool.call", OccurredAt: c.now(), TenantID: request.TenantID, ProjectID: request.ProjectID, RequestID: request.RequestID, Attributes: map[string]string{"tool": request.Request.Tool.Name, "session_id": request.SessionID, "task_id": request.TaskID, "agent_id": request.AgentID, "user_id": request.UserID}})
	}
	return &contracts.InvocationResponse{Response: &interaction.UnifiedResponse{ID: request.RequestID, ToolResult: result}}, nil
}

func mcpMethod(request *interaction.UnifiedRequest) string {
	method := mcpprotocol.Method(request)
	if method == "" {
		method = "tools/call"
	}
	return method
}

func (c *Connector) Stream(ctx context.Context, request contracts.InvocationRequest, writer contracts.StreamWriter) error {
	response, err := c.Invoke(ctx, request)
	if err != nil {
		return err
	}
	return writer.WriteChunk(ctx, contracts.StreamChunk{Event: interaction.StreamEvent{ID: response.Response.ID, Delta: response.Response.ToolResult.Content, Final: true}})
}
func (c *Connector) Health(context.Context, contracts.TargetRef) contracts.HealthStatus {
	return contracts.HealthStatus{Healthy: true, CheckedAt: c.now()}
}
func (c *Connector) Capabilities(context.Context, contracts.TargetRef) contracts.CapabilitySet {
	return contracts.CapabilitySet{"mcp": true, "tools/call": true, "server/discover": true, "tasks": true}
}
func (c *Connector) NormalizeError(err error) *contracts.UpstreamError {
	var upstream *contracts.UpstreamError
	if errors.As(err, &upstream) {
		return upstream
	}
	return &contracts.UpstreamError{Code: "MCP_ERROR", Retryable: true, Message: "MCP invocation failed"}
}

var _ contracts.InteractionInvoker = (*Connector)(nil)
