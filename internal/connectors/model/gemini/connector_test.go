package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

type secrets map[string][]byte

func (s secrets) Resolve(_ context.Context, ref string) ([]byte, error) {
	value, ok := s[ref]
	if !ok {
		return nil, errors.New("missing")
	}
	return value, nil
}

func TestInvokeGeminiGenerateContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-1.5-pro:generateContent" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("x-goog-api-key") != "provider-secret" {
			t.Fatalf("api key header=%q", r.Header.Get("x-goog-api-key"))
		}
		var body struct {
			Contents []struct {
				Role  string `json:"role"`
				Parts []struct {
					Text       string `json:"text"`
					InlineData *struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"contents"`
			SystemInstruction struct {
				Parts []struct{ Text string } `json:"parts"`
			} `json:"systemInstruction"`
			GenerationConfig map[string]any `json:"generationConfig"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.SystemInstruction.Parts[0].Text != "be brief" || body.Contents[0].Role != "user" || body.Contents[1].Role != "model" {
			t.Fatalf("body=%+v", body)
		}
		if body.Contents[0].Parts[1].InlineData == nil || body.Contents[0].Parts[1].InlineData.MimeType != "image/png" {
			t.Fatalf("inline data missing: %+v", body.Contents[0].Parts)
		}
		if body.GenerationConfig["maxOutputTokens"].(float64) != 32 {
			t.Fatalf("generation config=%+v", body.GenerationConfig)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"hello "},{"text":"gemini"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":3,"totalTokenCount":7}}`))
	}))
	defer server.Close()
	maxTokens := 32
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "gemini-1.5-pro", Chat: &interaction.ChatPayload{MaxOutputTokens: &maxTokens, Messages: []interaction.Message{
		{Role: "system", Content: "be brief"},
		{Role: "user", Content: "describe", Parts: []interaction.ContentPart{{Kind: interaction.ContentImage, MediaType: "image/png", DataURL: "data:image/png;base64,AAAA"}}},
		{Role: "assistant", Content: "previous"},
	}}}
	connector := connectorFor(t, server)
	result, err := connector.Invoke(context.Background(), contracts.InvocationRequest{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if result.Response.Choices[0].Message.Content != "hello gemini" || result.Response.Usage.TotalTokens() != 7 || result.Response.StopReason != "stop" {
		t.Fatalf("response=%+v", result.Response)
	}
}

func TestGeminiFailuresAndCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("provider-secret"))
	}))
	defer server.Close()
	connector := connectorFor(t, server)
	_, err := connector.Invoke(context.Background(), contracts.InvocationRequest{Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "m", Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "hi"}}}}})
	normalized := connector.NormalizeError(err)
	if normalized.StatusCode != http.StatusTooManyRequests || !normalized.Retryable || strings.Contains(err.Error(), "provider-secret") {
		t.Fatalf("err=%v normalized=%+v", err, normalized)
	}
	if err := connector.Stream(context.Background(), contracts.InvocationRequest{}, nil); err == nil {
		t.Fatal("stream accepted invalid request")
	}
	caps := connector.Capabilities(context.Background(), contracts.TargetRef{})
	if !caps["chat"] || !caps["stream"] || !caps["tools"] {
		t.Fatalf("capability missing: %+v", caps)
	}
}

func TestInvokeGeminiWithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Contents []struct {
				Role  string `json:"role"`
				Parts []struct {
					FunctionCall *struct {
						Name string         `json:"name"`
						Args map[string]any `json:"args"`
					} `json:"functionCall"`
					FunctionResponse *struct {
						Name     string         `json:"name"`
						Response map[string]any `json:"response"`
					} `json:"functionResponse"`
				} `json:"parts"`
			} `json:"contents"`
			Tools []struct {
				FunctionDeclarations []struct {
					Name       string         `json:"name"`
					Parameters map[string]any `json:"parameters"`
				} `json:"functionDeclarations"`
			} `json:"tools"`
			ToolConfig struct {
				FunctionCallingConfig struct {
					Mode                 string   `json:"mode"`
					AllowedFunctionNames []string `json:"allowedFunctionNames"`
				} `json:"functionCallingConfig"`
			} `json:"toolConfig"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Tools) != 1 || body.Tools[0].FunctionDeclarations[0].Name != "get_weather" || body.Tools[0].FunctionDeclarations[0].Parameters["type"] != "object" {
			t.Fatalf("tools=%+v", body.Tools)
		}
		if body.ToolConfig.FunctionCallingConfig.Mode != "ANY" || body.ToolConfig.FunctionCallingConfig.AllowedFunctionNames[0] != "get_weather" {
			t.Fatalf("toolConfig=%+v", body.ToolConfig)
		}
		if body.Contents[1].Role != "model" || body.Contents[1].Parts[0].FunctionCall.Name != "get_weather" || body.Contents[1].Parts[0].FunctionCall.Args["city"] != "Paris" {
			t.Fatalf("assistant tool call=%+v", body.Contents[1])
		}
		if body.Contents[2].Parts[0].FunctionResponse.Name != "get_weather" || body.Contents[2].Parts[0].FunctionResponse.Response["content"] != "sunny" {
			t.Fatalf("tool response=%+v", body.Contents[2])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","args":{"city":"Paris"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2}}`))
	}))
	defer server.Close()
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "gemini-1.5-pro", Chat: &interaction.ChatPayload{
		Messages: []interaction.Message{
			{Role: "user", Content: "weather"},
			{Role: "assistant", ToolCalls: []interaction.ToolCall{{ID: "call_9", Type: "function", Function: interaction.ToolCallFunction{Name: "get_weather", Arguments: `{"city":"Paris"}`}}}},
			{Role: "tool", ToolCallID: "call_9", Content: "sunny"},
		},
		Tools:      []interaction.Tool{{Type: "function", Function: interaction.ToolFunction{Name: "get_weather", Parameters: json.RawMessage(`{"type":"object"}`)}}},
		ToolChoice: json.RawMessage(`{"type":"function","function":{"name":"get_weather"}}`),
	}}
	result, err := connectorFor(t, server).Invoke(context.Background(), contracts.InvocationRequest{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	calls := result.Response.Choices[0].Message.ToolCalls
	if result.Response.StopReason != "tool_calls" || len(calls) != 1 || calls[0].ID != "gemini_call_0" || calls[0].Function.Arguments != `{"city":"Paris"}` {
		t.Fatalf("response=%+v", result.Response)
	}
}

func TestStreamGeminiGenerateContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-1.5-flash:streamGenerateContent" || r.URL.Query().Get("alt") != "sse" {
			t.Fatalf("url=%s", r.URL.String())
		}
		if r.Header.Get("Accept") != "text/event-stream" || r.Header.Get("x-goog-api-key") != "provider-secret" {
			t.Fatalf("headers=%v", r.Header)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hel\"}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"lo\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":1,\"totalTokenCount\":3}}\n\n"))
	}))
	defer server.Close()
	connector := connectorFor(t, server)
	writer := &streamRecorder{}
	err := connector.Stream(context.Background(), contracts.InvocationRequest{Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "gemini-1.5-flash", Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "hi"}}}}}, writer)
	if err != nil {
		t.Fatal(err)
	}
	if len(writer.events) != 3 || writer.events[0].Delta != "hel" || writer.events[1].Delta != "lo" || writer.events[1].StopReason != "stop" || !writer.events[2].Final {
		t.Fatalf("events=%+v", writer.events)
	}
	if writer.events[1].Usage == nil || writer.events[1].Usage.TotalTokens() != 3 {
		t.Fatalf("usage=%+v", writer.events[1].Usage)
	}
}

type streamRecorder struct{ events []interaction.StreamEvent }

func (r *streamRecorder) WriteChunk(_ context.Context, chunk contracts.StreamChunk) error {
	r.events = append(r.events, chunk.Event)
	return nil
}

func connectorFor(t *testing.T, server *httptest.Server) *Connector {
	t.Helper()
	config := DefaultConfig()
	config.BaseURL = server.URL
	config.SecretRef = "secret://provider"
	config.AllowInsecureHTTP = true
	config.RequestTimeout = time.Second
	connector, err := New(config, server.Client(), secrets{"secret://provider": []byte("provider-secret")})
	if err != nil {
		t.Fatal(err)
	}
	return connector
}
