package azure

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

type memorySecrets map[string][]byte

func (m memorySecrets) Resolve(_ context.Context, ref string) ([]byte, error) {
	return m[ref], nil
}

func newTestAzure(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Connector) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	connector, err := New(Config{BaseURL: server.URL, SecretRef: "secret://azure", APIVersion: "2024-06-01", AllowInsecureHTTP: true, RequestTimeout: 5e9, HealthTimeout: 1e9, Capabilities: contracts.CapabilitySet{"chat": true, "stream": true}}, server.Client(), memorySecrets{"secret://azure": []byte("azure-key")})
	if err != nil {
		t.Fatal(err)
	}
	return server, connector
}

func TestAzureInvokeUsesDeploymentPathAndAPIKey(t *testing.T) {
	var gotPath, gotKey string
	_, connector := newTestAzure(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("api-key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"az1","model":"deployment-gpt","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	})
	resp, err := connector.Invoke(context.Background(), contracts.InvocationRequest{Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "deployment-gpt", Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "hi"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Response.Choices[0].Message.Content != "ok" || resp.Response.Model != "deployment-gpt" {
		t.Fatalf("response = %+v", resp.Response)
	}
	if gotPath != "/openai/deployments/deployment-gpt/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotKey != "azure-key" {
		t.Fatalf("api-key = %q", gotKey)
	}
}

func TestAzureInvokeWithCachedTokens(t *testing.T) {
	_, connector := newTestAzure(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"az2","model":"deployment-gpt","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_tokens_details":{"cached_tokens":90}}}`))
	})
	resp, err := connector.Invoke(context.Background(), contracts.InvocationRequest{Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "deployment-gpt", Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "hi"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Response.Usage.CacheReadTokens != 90 || resp.Response.Usage.CachedInputTokens != 90 {
		t.Fatalf("cached tokens not parsed: %+v", resp.Response.Usage)
	}
}

func TestAzureStreamBodiesDeployment(t *testing.T) {
	var gotPath, gotStream bool
	_, connector := newTestAzure(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path == "/openai/deployments/deployment-gpt/chat/completions"
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotStream, _ = body["stream"].(bool)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"a\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	})
	err := connector.Stream(context.Background(), contracts.InvocationRequest{Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "deployment-gpt", Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "hi"}}}}}, streamSink(t))
	if err != nil {
		t.Fatal(err)
	}
	if !gotPath || !gotStream {
		t.Fatalf("path=%t stream=%t", gotPath, gotStream)
	}
}

// streamSink implements the minimal StreamWriter a test needs.
func streamSink(t *testing.T) contracts.StreamWriter {
	t.Helper()
	var writer streamWriterSpy
	return &writer
}

type streamWriterSpy struct{ chunks []contracts.StreamChunk }

func (s *streamWriterSpy) WriteChunk(ctx context.Context, chunk contracts.StreamChunk) error {
	s.chunks = append(s.chunks, chunk)
	return nil
}
