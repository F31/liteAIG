// Package liteaig is the official Go client for LiteAIG's public Gateway API.
package liteaig

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultAPIVersion = "2026-09-09"
	apiVersionHeader  = "LiteAIG-API-Version"
)

type Client struct {
	baseURL    string
	apiKey     string
	apiVersion string
	httpClient *http.Client
}

type Option func(*Client)

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func WithAPIVersion(version string) Option {
	return func(c *Client) { c.apiVersion = strings.TrimSpace(version) }
}

func New(baseURL, apiKey string, opts ...Option) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("liteaig: baseURL and apiKey are required")
	}
	c := &Client{baseURL: baseURL, apiKey: apiKey, apiVersion: DefaultAPIVersion, httpClient: &http.Client{Timeout: 30 * time.Second}}
	for _, opt := range opts {
		opt(c)
	}
	if c.apiVersion == "" {
		c.apiVersion = DefaultAPIVersion
	}
	return c, nil
}

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type ChatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

type ChatCompletionResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}

type ChatCompletionStreamEvent struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string          `json:"role,omitempty"`
			Content   string          `json:"content,omitempty"`
			ToolCalls []ToolCallDelta `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage,omitempty"`
}

type ToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type ChatCompletionStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	done    bool
}

type ResponsesRequest struct {
	Model        string `json:"model"`
	Input        any    `json:"input"`
	Instructions string `json:"instructions,omitempty"`
}

type ResponsesResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Model  string `json:"model"`
	Output []struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage ResponsesUsage `json:"usage"`
}

func (r ResponsesResponse) OutputText() string {
	var b strings.Builder
	for _, item := range r.Output {
		for _, part := range item.Content {
			if part.Type == "output_text" {
				b.WriteString(part.Text)
			}
		}
	}
	return b.String()
}

type EmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type EmbeddingResponse struct {
	Model string `json:"model"`
	Data  []struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Usage Usage `json:"usage"`
}

type MCPRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *MCPError       `json:"error,omitempty"`
}

type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type MCPToolCallParams struct {
	Name      string `json:"name"`
	Arguments any    `json:"arguments,omitempty"`
}

type A2ARequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type A2AResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *MCPError       `json:"error,omitempty"`
}

type A2AMessage struct {
	MessageID string            `json:"messageId"`
	Role      string            `json:"role"`
	Parts     []A2APart         `json:"parts"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type A2APart struct {
	Kind string `json:"kind,omitempty"`
	Text string `json:"text"`
}

type A2ASendMessageParams struct {
	Message A2AMessage `json:"message"`
}

type A2AStreamEvent struct {
	Delta string
}

type A2AStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	done    bool
}

type ModelList struct {
	Object string `json:"object"`
	Data   []struct {
		ID string `json:"id"`
	} `json:"data"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ResponsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

func (c *Client) CreateChatCompletion(ctx context.Context, request ChatCompletionRequest) (*ChatCompletionResponse, error) {
	var response ChatCompletionResponse
	if err := c.post(ctx, "/v1/chat/completions", request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) CreateChatCompletionStream(ctx context.Context, request ChatCompletionRequest) (*ChatCompletionStream, error) {
	request.Stream = true
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	resp, err := c.raw(ctx, http.MethodPost, "/v1/chat/completions", body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("liteaig: %s: %s", resp.Status, strings.TrimSpace(string(buf)))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &ChatCompletionStream{body: resp.Body, scanner: scanner}, nil
}

func (c *Client) CreateResponse(ctx context.Context, request ResponsesRequest) (*ResponsesResponse, error) {
	var response ResponsesResponse
	if err := c.post(ctx, "/v1/responses", request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) CreateEmbedding(ctx context.Context, request EmbeddingRequest) (*EmbeddingResponse, error) {
	var response EmbeddingResponse
	if err := c.post(ctx, "/v1/embeddings", request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) CallMCP(ctx context.Context, request MCPRequest) (*MCPResponse, error) {
	if request.JSONRPC == "" {
		request.JSONRPC = "2.0"
	}
	var response MCPResponse
	if err := c.post(ctx, "/mcp", request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) CallMCPTool(ctx context.Context, id any, name string, arguments any) (*MCPResponse, error) {
	return c.CallMCP(ctx, MCPRequest{ID: id, Method: "tools/call", Params: MCPToolCallParams{Name: name, Arguments: arguments}})
}

func (c *Client) CallA2A(ctx context.Context, request A2ARequest) (*A2AResponse, error) {
	if request.JSONRPC == "" {
		request.JSONRPC = "2.0"
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	resp, err := c.raw(ctx, http.MethodPost, "/a2a", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var response A2AResponse
		if json.Unmarshal(buf, &response) == nil && response.Error != nil {
			return &response, nil
		}
		return nil, fmt.Errorf("liteaig: %s: %s", resp.Status, strings.TrimSpace(string(buf)))
	}
	var response A2AResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) SendA2AMessage(ctx context.Context, id any, message A2AMessage) (*A2AResponse, error) {
	return c.CallA2A(ctx, A2ARequest{ID: id, Method: "SendMessage", Params: A2ASendMessageParams{Message: message}})
}

func (c *Client) SendA2AMessageStream(ctx context.Context, id any, message A2AMessage) (*A2AStream, error) {
	request := A2ARequest{JSONRPC: "2.0", ID: id, Method: "SendStreamingMessage", Params: A2ASendMessageParams{Message: message}}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	resp, err := c.raw(ctx, http.MethodPost, "/a2a", body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var response A2AResponse
		if json.Unmarshal(buf, &response) == nil && response.Error != nil {
			return nil, fmt.Errorf("liteaig: a2a error %d: %s", response.Error.Code, response.Error.Message)
		}
		return nil, fmt.Errorf("liteaig: %s: %s", resp.Status, strings.TrimSpace(string(buf)))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &A2AStream{body: resp.Body, scanner: scanner}, nil
}

func (c *Client) ListModels(ctx context.Context) (*ModelList, error) {
	var response ModelList
	if err := c.do(ctx, http.MethodGet, "/v1/models", nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) post(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, path, body, out)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, out any) error {
	resp, err := c.raw(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("liteaig: %s: %s", resp.Status, strings.TrimSpace(string(buf)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) raw(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set(apiVersionHeader, c.apiVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

func (s *ChatCompletionStream) Recv() (*ChatCompletionStreamEvent, error) {
	if s.done {
		return nil, io.EOF
	}
	for s.scanner.Scan() {
		line := strings.TrimSpace(s.scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			s.done = true
			_ = s.body.Close()
			return nil, io.EOF
		}
		var event ChatCompletionStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil, err
		}
		return &event, nil
	}
	s.done = true
	_ = s.body.Close()
	if err := s.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

func (s *ChatCompletionStream) Close() error {
	s.done = true
	return s.body.Close()
}

func (s *A2AStream) Recv() (*A2AStreamEvent, error) {
	if s.done {
		return nil, io.EOF
	}
	for s.scanner.Scan() {
		line := strings.TrimSpace(s.scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			s.done = true
			_ = s.body.Close()
			return nil, io.EOF
		}
		delta, completed, err := parseA2AStreamFrame([]byte(data))
		if err != nil {
			s.done = true
			_ = s.body.Close()
			return nil, err
		}
		if completed {
			s.done = true
			_ = s.body.Close()
			return nil, io.EOF
		}
		if delta == "" {
			continue
		}
		return &A2AStreamEvent{Delta: delta}, nil
	}
	s.done = true
	_ = s.body.Close()
	if err := s.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

func (s *A2AStream) Close() error {
	s.done = true
	return s.body.Close()
}

func parseA2AStreamFrame(data []byte) (string, bool, error) {
	var frame struct {
		Type    string `json:"type"`
		Message *struct {
			Parts []A2APart `json:"parts"`
		} `json:"message"`
		Error *MCPError `json:"error"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		return "", false, err
	}
	if frame.Error != nil {
		return "", false, fmt.Errorf("a2a stream error: %s", frame.Error.Message)
	}
	if frame.Type == "completed" {
		return "", true, nil
	}
	if frame.Type != "message" || frame.Message == nil {
		return "", false, nil
	}
	var text strings.Builder
	for _, part := range frame.Message.Parts {
		if part.Kind != "" && part.Kind != "text" {
			continue
		}
		if text.Len() > 0 {
			text.WriteString("\n")
		}
		text.WriteString(part.Text)
	}
	return text.String(), false, nil
}
