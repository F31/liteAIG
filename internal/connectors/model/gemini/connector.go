// Package gemini invokes Google's native Gemini generateContent API.
package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

type SecretResolver interface {
	Resolve(context.Context, string) ([]byte, error)
}

type Config struct {
	BaseURL, SecretRef string
	RequestTimeout     time.Duration
	HealthTimeout      time.Duration
	AllowInsecureHTTP  bool
	Capabilities       contracts.CapabilitySet
	Parameters         interaction.ParameterSupport
}

func DefaultConfig() Config {
	return Config{BaseURL: "https://generativelanguage.googleapis.com", RequestTimeout: 30 * time.Second, HealthTimeout: 2 * time.Second, Capabilities: contracts.CapabilitySet{"chat": true, "stream": true, "tools": true}}
}

type Connector struct {
	config  Config
	client  *http.Client
	secrets SecretResolver
}

func New(config Config, client *http.Client, secrets SecretResolver) (*Connector, error) {
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || parsed.Host == "" {
		return nil, errors.New("valid Gemini base URL is required")
	}
	if parsed.Scheme != "https" && !config.AllowInsecureHTTP {
		return nil, errors.New("Gemini connector requires HTTPS")
	}
	if config.SecretRef == "" || config.RequestTimeout <= 0 || config.HealthTimeout <= 0 || client == nil || secrets == nil {
		return nil, errors.New("complete Gemini connector configuration is required")
	}
	return &Connector{config: config, client: client, secrets: secrets}, nil
}

func (c *Connector) Invoke(ctx context.Context, request contracts.InvocationRequest) (*contracts.InvocationResponse, error) {
	if request.Request == nil || request.Request.Kind != interaction.RequestChat && request.Request.Kind != interaction.RequestResponses {
		return nil, errors.New("Gemini connector requires chat request")
	}
	body, err := encode(request.Request)
	if err != nil {
		return nil, err
	}
	response, err := c.do(ctx, request.Request.Model, body, false)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, statusError(response)
	}
	canonical, err := decode(response.Body)
	if err != nil {
		return nil, err
	}
	return &contracts.InvocationResponse{Response: canonical}, nil
}

func (c *Connector) Stream(ctx context.Context, request contracts.InvocationRequest, writer contracts.StreamWriter) error {
	if request.Request == nil || request.Request.Kind != interaction.RequestChat && request.Request.Kind != interaction.RequestResponses {
		return errors.New("Gemini stream requires chat request")
	}
	body, err := encode(request.Request)
	if err != nil {
		return err
	}
	response, err := c.do(ctx, request.Request.Model, body, true)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return statusError(response)
	}
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		event, err := decodeStreamEvent([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))))
		if err != nil {
			return err
		}
		if event == nil {
			continue
		}
		if err := writer.WriteChunk(ctx, contracts.StreamChunk{Event: *event}); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return writer.WriteChunk(ctx, contracts.StreamChunk{Event: interaction.StreamEvent{Final: true}})
}

func (c *Connector) Health(ctx context.Context, _ contracts.TargetRef) contracts.HealthStatus {
	started := time.Now()
	healthCtx, cancel := context.WithTimeout(ctx, c.config.HealthTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(healthCtx, http.MethodHead, strings.TrimRight(c.config.BaseURL, "/"), nil)
	if err == nil {
		response, callErr := c.client.Do(request)
		err = callErr
		if response != nil {
			response.Body.Close()
			if response.StatusCode >= 500 {
				err = errors.New("upstream unavailable")
			}
		}
	}
	return contracts.HealthStatus{Healthy: err == nil, CheckedAt: started, Reason: errorText(err)}
}

func (c *Connector) Capabilities(context.Context, contracts.TargetRef) contracts.CapabilitySet {
	return cloneCapabilities(c.config.Capabilities)
}

func (c *Connector) SupportedParams() interaction.ParameterSupport { return c.config.Parameters }

func (c *Connector) NormalizeError(err error) *contracts.UpstreamError {
	var upstream *contracts.UpstreamError
	if errors.As(err, &upstream) {
		return upstream
	}
	return &contracts.UpstreamError{Code: "upstream_transport_error", Retryable: errors.Is(err, context.DeadlineExceeded), Message: "upstream request failed"}
}

func (c *Connector) do(ctx context.Context, model string, body []byte, stream bool) (*http.Response, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	secret, err := c.secrets.Resolve(callCtx, c.config.SecretRef)
	if err != nil {
		cancel()
		return nil, errors.New("resolve provider credential")
	}
	secretCopy := append([]byte(nil), secret...)
	defer clear(secretCopy)
	method := ":generateContent"
	if stream {
		method = ":streamGenerateContent?alt=sse"
	}
	path := "/v1beta/models/" + url.PathEscape(model) + method
	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, strings.TrimRight(c.config.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-goog-api-key", string(secretCopy))
	if stream {
		request.Header.Set("Accept", "text/event-stream")
	}
	response, err := c.client.Do(request)
	if err != nil {
		cancel()
		return nil, err
	}
	response.Body = &cancelBody{ReadCloser: response.Body, cancel: cancel}
	return response, nil
}

func encode(request *interaction.UnifiedRequest) ([]byte, error) {
	if request.Chat == nil {
		return nil, errors.New("chat payload is required")
	}
	var wire struct {
		Contents          []content      `json:"contents"`
		SystemInstruction *content       `json:"systemInstruction,omitempty"`
		GenerationConfig  map[string]any `json:"generationConfig,omitempty"`
		Tools             []geminiTool   `json:"tools,omitempty"`
		ToolConfig        any            `json:"toolConfig,omitempty"`
	}
	callNames := map[string]string{}
	for _, message := range request.Chat.Messages {
		if message.Role == "system" {
			wire.SystemInstruction = &content{Parts: []part{{Text: message.Content}}}
			continue
		}
		wire.Contents = append(wire.Contents, content{Role: geminiRole(message.Role), Parts: geminiParts(message, callNames)})
	}
	if len(request.Chat.Tools) > 0 {
		declarations := make([]functionDeclaration, 0, len(request.Chat.Tools))
		for _, item := range request.Chat.Tools {
			declarations = append(declarations, functionDeclaration{Name: item.Function.Name, Description: item.Function.Description, Parameters: item.Function.Parameters})
		}
		wire.Tools = []geminiTool{{FunctionDeclarations: declarations}}
	}
	if choice, ok := geminiToolChoice(request.Chat.ToolChoice); ok {
		wire.ToolConfig = choice
	}
	config := map[string]any{}
	if request.Chat.MaxOutputTokens != nil {
		config["maxOutputTokens"] = *request.Chat.MaxOutputTokens
	}
	if request.Chat.Temperature != nil {
		config["temperature"] = *request.Chat.Temperature
	}
	if request.Chat.TopP != nil {
		config["topP"] = *request.Chat.TopP
	}
	if len(config) > 0 {
		wire.GenerationConfig = config
	}
	return json.Marshal(wire)
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type part struct {
	Text             string            `json:"text,omitempty"`
	InlineData       *inlineData       `json:"inlineData,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
}

type inlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiTool struct {
	FunctionDeclarations []functionDeclaration `json:"functionDeclarations"`
}

type functionDeclaration struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type functionCall struct {
	Name string          `json:"name,omitempty"`
	Args json.RawMessage `json:"args,omitempty"`
}

type functionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

func geminiParts(message interaction.Message, callNames map[string]string) []part {
	parts := []part{}
	if message.Role == "tool" {
		name := callNames[message.ToolCallID]
		if name == "" {
			name = message.ToolCallID
		}
		return []part{{FunctionResponse: &functionResponse{Name: name, Response: map[string]any{"content": message.Content}}}}
	}
	if message.Content != "" {
		parts = append(parts, part{Text: message.Content})
	}
	for _, call := range message.ToolCalls {
		args := json.RawMessage("{}")
		if call.Function.Arguments != "" {
			args = json.RawMessage(call.Function.Arguments)
		}
		if call.ID != "" {
			callNames[call.ID] = call.Function.Name
		}
		parts = append(parts, part{FunctionCall: &functionCall{Name: call.Function.Name, Args: args}})
	}
	for _, item := range message.Parts {
		if item.Kind != interaction.ContentImage || item.DataURL == "" {
			continue
		}
		mime, data, ok := splitDataURL(item.DataURL)
		if !ok {
			continue
		}
		if item.MediaType != "" {
			mime = item.MediaType
		}
		parts = append(parts, part{InlineData: &inlineData{MimeType: mime, Data: data}})
	}
	return parts
}

func geminiToolChoice(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		mode := map[string]string{"auto": "AUTO", "required": "ANY", "none": "NONE"}[text]
		if mode == "" {
			return nil, false
		}
		return map[string]any{"functionCallingConfig": map[string]any{"mode": mode}}, true
	}
	var named struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &named); err == nil && named.Type == "function" && named.Function.Name != "" {
		return map[string]any{"functionCallingConfig": map[string]any{"mode": "ANY", "allowedFunctionNames": []string{named.Function.Name}}}, true
	}
	var passthrough any
	if err := json.Unmarshal(raw, &passthrough); err == nil {
		return passthrough, true
	}
	return nil, false
}

func splitDataURL(value string) (string, string, bool) {
	if !strings.HasPrefix(value, "data:") {
		return "", "", false
	}
	meta, data, ok := strings.Cut(strings.TrimPrefix(value, "data:"), ",")
	if !ok || data == "" {
		return "", "", false
	}
	mime := strings.TrimSuffix(meta, ";base64")
	if mime == "" {
		mime = "application/octet-stream"
	}
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		return "", "", false
	}
	return mime, data, true
}

func geminiRole(role string) string {
	if role == "assistant" {
		return "model"
	}
	return "user"
}

func decode(reader io.Reader) (*interaction.UnifiedResponse, error) {
	var wire struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string        `json:"text"`
					FunctionCall *functionCall `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		Usage struct {
			PromptTokens     int64 `json:"promptTokenCount"`
			CompletionTokens int64 `json:"candidatesTokenCount"`
			TotalTokens      int64 `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.NewDecoder(reader).Decode(&wire); err != nil {
		return nil, err
	}
	response := &interaction.UnifiedResponse{Usage: interaction.UnifiedUsage{InputTokens: wire.Usage.PromptTokens, OutputTokens: wire.Usage.CompletionTokens}}
	if response.Usage.InputTokens+response.Usage.OutputTokens == 0 && wire.Usage.TotalTokens > 0 {
		response.Usage.OutputTokens = wire.Usage.TotalTokens
	}
	for i, candidate := range wire.Candidates {
		var text strings.Builder
		var calls []interaction.ToolCall
		for _, part := range candidate.Content.Parts {
			text.WriteString(part.Text)
			if part.FunctionCall != nil {
				calls = append(calls, interaction.ToolCall{ID: "gemini_call_" + strconv.Itoa(len(calls)), Type: "function", Function: interaction.ToolCallFunction{Name: part.FunctionCall.Name, Arguments: rawJSONOrEmpty(part.FunctionCall.Args)}})
			}
		}
		response.Choices = append(response.Choices, interaction.Choice{Index: i, Message: interaction.Message{Role: "assistant", Content: text.String(), ToolCalls: calls}})
		if response.StopReason == "" {
			response.StopReason = strings.ToLower(candidate.FinishReason)
			if len(calls) > 0 && (response.StopReason == "" || response.StopReason == "stop") {
				response.StopReason = "tool_calls"
			}
		}
	}
	return response, nil
}

func decodeStreamEvent(data []byte) (*interaction.StreamEvent, error) {
	if len(data) == 0 || string(data) == "[DONE]" {
		return nil, nil
	}
	var wire struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string        `json:"text"`
					FunctionCall *functionCall `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		Usage struct {
			PromptTokens     int64 `json:"promptTokenCount"`
			CompletionTokens int64 `json:"candidatesTokenCount"`
			TotalTokens      int64 `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, err
	}
	event := &interaction.StreamEvent{}
	if len(wire.Candidates) > 0 {
		candidate := wire.Candidates[0]
		for _, part := range candidate.Content.Parts {
			event.Delta += part.Text
			if part.FunctionCall != nil {
				event.ToolCallDeltas = append(event.ToolCallDeltas, interaction.ToolCallDelta{Index: len(event.ToolCallDeltas), ID: "gemini_call_" + strconv.Itoa(len(event.ToolCallDeltas)), Type: "function", Name: part.FunctionCall.Name, Arguments: rawJSONOrEmpty(part.FunctionCall.Args)})
			}
		}
		event.StopReason = strings.ToLower(candidate.FinishReason)
		if len(event.ToolCallDeltas) > 0 && (event.StopReason == "" || event.StopReason == "stop") {
			event.StopReason = "tool_calls"
		}
	}
	if wire.Usage.PromptTokens+wire.Usage.CompletionTokens+wire.Usage.TotalTokens > 0 {
		event.Usage = &interaction.UnifiedUsage{InputTokens: wire.Usage.PromptTokens, OutputTokens: wire.Usage.CompletionTokens}
		if event.Usage.InputTokens+event.Usage.OutputTokens == 0 && wire.Usage.TotalTokens > 0 {
			event.Usage.OutputTokens = wire.Usage.TotalTokens
		}
	}
	if event.Delta == "" && event.StopReason == "" && event.Usage == nil {
		return nil, nil
	}
	return event, nil
}

func rawJSONOrEmpty(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

func statusError(response *http.Response) error {
	io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return &contracts.UpstreamError{Code: "upstream_http_error", StatusCode: response.StatusCode, Retryable: response.StatusCode == 429 || response.StatusCode >= 500, Message: http.StatusText(response.StatusCode)}
}

type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelBody) Close() error { err := b.ReadCloser.Close(); b.cancel(); return err }

func cloneCapabilities(input contracts.CapabilitySet) contracts.CapabilitySet {
	result := make(contracts.CapabilitySet, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

var _ contracts.InteractionInvoker = (*Connector)(nil)
