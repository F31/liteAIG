// Package mcp normalizes MCP 2026-07-28 Streamable HTTP messages into tool interactions.
package mcp

import (
	"encoding/json"
	"errors"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

const ProtocolVersion = "2026-07-28"

const metadataMethod = "mcp_method"

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func Normalize(raw []byte) (*interaction.UnifiedRequest, error) {
	var wire Request
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, err
	}
	if wire.JSONRPC != "2.0" {
		return nil, errors.New("invalid MCP jsonrpc version")
	}
	if wire.Method == "server/discover" {
		return &interaction.UnifiedRequest{Kind: interaction.RequestTool, Metadata: map[string]string{metadataMethod: wire.Method}, Tool: &interaction.ToolPayload{Name: "server/discover", Arguments: wire.Params}}, nil
	}
	if len(wire.Method) > len("tasks/") && wire.Method[:len("tasks/")] == "tasks/" {
		return &interaction.UnifiedRequest{Kind: interaction.RequestTool, Model: wire.Method, Metadata: map[string]string{metadataMethod: wire.Method}, Tool: &interaction.ToolPayload{Name: wire.Method, Arguments: wire.Params}}, nil
	}
	if wire.Method != "tools/call" {
		return nil, errors.New("unsupported MCP method")
	}
	var params ToolCallParams
	if err := json.Unmarshal(wire.Params, &params); err != nil {
		return nil, err
	}
	if params.Name == "" {
		return nil, errors.New("MCP tool name is required")
	}
	return &interaction.UnifiedRequest{Kind: interaction.RequestTool, Model: params.Name, Metadata: map[string]string{metadataMethod: wire.Method}, Tool: &interaction.ToolPayload{Name: params.Name, Arguments: params.Arguments}}, nil
}

// Method returns the original MCP JSON-RPC method captured during Normalize.
func Method(request *interaction.UnifiedRequest) string {
	if request == nil || request.Metadata == nil {
		return ""
	}
	return request.Metadata[metadataMethod]
}
