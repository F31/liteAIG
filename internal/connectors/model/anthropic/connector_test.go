package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

type writer struct{ events []interaction.StreamEvent }

func (w *writer) WriteChunk(_ context.Context, chunk contracts.StreamChunk) error {
	w.events = append(w.events, chunk.Event)
	return nil
}
func chatRequest() *interaction.UnifiedRequest {
	max := 16
	return &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "claude", Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "hi"}}, MaxOutputTokens: &max}}
}
func connectorFor(t *testing.T, server *httptest.Server, timeout time.Duration) *Connector {
	t.Helper()
	config := DefaultConfig()
	config.BaseURL = server.URL
	config.SecretRef = "secret://provider"
	config.RequestTimeout = timeout
	config.AllowInsecureHTTP = true
	connector, err := New(config, server.Client(), secrets{"secret://provider": []byte("provider-secret")})
	if err != nil {
		t.Fatal(err)
	}
	return connector
}
func TestInvokeNormalAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "provider-secret" || r.Header.Get("anthropic-version") == "" {
			t.Error("missing Anthropic headers")
		}
		w.Write([]byte(`{"id":"r1","model":"claude","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`))
	}))
	defer server.Close()
	result, err := connectorFor(t, server, time.Second).Invoke(context.Background(), contracts.InvocationRequest{Request: chatRequest()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Response.Usage.TotalTokens() != 5 || result.Response.Choices[0].Message.Content != "ok" {
		t.Fatalf("response=%+v", result.Response)
	}
}

func TestInvokePassesPromptCacheControl(t *testing.T) {
	seen := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		seen <- raw
		w.Write([]byte(`{"id":"r1","model":"claude","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2,"cache_read_input_tokens":3}}`))
	}))
	defer server.Close()
	max := 16
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "claude", Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "system", Content: "remember this", CacheControl: "ephemeral"}, {Role: "user", Content: "hi", CacheControl: "ephemeral"}}, MaxOutputTokens: &max}}
	if _, err := connectorFor(t, server, time.Second).Invoke(context.Background(), contracts.InvocationRequest{Request: request}); err != nil {
		t.Fatal(err)
	}
	var wire struct {
		System []struct {
			Type         string `json:"type"`
			Text         string `json:"text"`
			CacheControl *struct {
				Type string `json:"type"`
			} `json:"cache_control"`
		} `json:"system"`
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type         string `json:"type"`
				Text         string `json:"text"`
				CacheControl *struct {
					Type string `json:"type"`
				} `json:"cache_control"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(<-seen, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.System) != 1 || wire.System[0].CacheControl == nil || wire.System[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("system cache_control not sent: %+v", wire.System)
	}
	if len(wire.Messages) != 1 || len(wire.Messages[0].Content) != 1 || wire.Messages[0].Content[0].CacheControl == nil || wire.Messages[0].Content[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("message cache_control not sent: %+v", wire.Messages)
	}
}

func TestFailuresTimeoutAndCancel(t *testing.T) {
	for _, status := range []int{429, 500} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			w.Write([]byte("provider-secret"))
		}))
		connector := connectorFor(t, server, time.Second)
		_, err := connector.Invoke(context.Background(), contracts.InvocationRequest{Request: chatRequest()})
		server.Close()
		normalized := connector.NormalizeError(err)
		if normalized.StatusCode != status || !normalized.Retryable || strings.Contains(err.Error(), "provider-secret") {
			t.Fatalf("status=%d error=%v normalized=%+v", status, err, normalized)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
	}))
	defer server.Close()
	connector := connectorFor(t, server, 20*time.Millisecond)
	if _, err := connector.Invoke(context.Background(), contracts.InvocationRequest{Request: chatRequest()}); err == nil || !connector.NormalizeError(err).Retryable {
		t.Fatalf("timeout error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := connector.Invoke(ctx, contracts.InvocationRequest{Request: chatRequest()}); err == nil {
		t.Fatal("cancelled request succeeded")
	}
}
func TestStreamAndDrop(t *testing.T) {
	complete := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer complete.Close()
	output := &writer{}
	if err := connectorFor(t, complete, time.Second).Stream(context.Background(), contracts.InvocationRequest{Request: chatRequest()}, output); err != nil || len(output.events) != 2 {
		t.Fatalf("Stream()=%+v,%v", output.events, err)
	}
	dropped := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"partial\"}}\n"))
	}))
	defer dropped.Close()
	if err := connectorFor(t, dropped, time.Second).Stream(context.Background(), contracts.InvocationRequest{Request: chatRequest()}, &writer{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("dropped error=%v", err)
	}
}
