package bedrock

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

type memorySecrets map[string][]byte

func (m memorySecrets) Resolve(_ context.Context, ref string) ([]byte, error) {
	return m[ref], nil
}

const awsSecret = `{"access_key_id":"AKIDEXAMPLE","secret_access_key":"wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}`

func intPtr(value int) *int { return &value }

func newTestBedrock(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Connector) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	connector, err := New(Config{BaseURL: server.URL, Region: "us-east-1", SecretRef: "secret://aws", AllowInsecureHTTP: true, RequestTimeout: 5e9, HealthTimeout: 1e9, Capabilities: contracts.CapabilitySet{"chat": true}}, server.Client(), memorySecrets{"secret://aws": []byte(awsSecret)})
	if err != nil {
		t.Fatal(err)
	}
	return server, connector
}

func TestBedrockInvokeAnthropicCompatible(t *testing.T) {
	var gotPath string
	var gotBody struct {
		AnthropicVersion string `json:"anthropic_version"`
		Model            any    `json:"model"`
		Messages         []any  `json:"messages"`
		MaxTokens        int    `json:"max_tokens"`
	}
	var gotAuth string
	_, connector := newTestBedrock(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg-bedrock","type":"message","role":"assistant","model":"anthropic.claude-3-5-haiku-20241022-v1:0","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":8,"cache_creation_input_tokens":2}}`))
	})
	resp, err := connector.Invoke(context.Background(), contracts.InvocationRequest{Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "anthropic.claude-3-5-haiku-20241022-v1:0", Chat: &interaction.ChatPayload{MaxOutputTokens: intPtr(64), Messages: []interaction.Message{{Role: "user", Content: "hi"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Response.Choices[0].Message.Content != "ok" {
		t.Fatalf("response = %+v", resp.Response)
	}
	// Cache fields from the Anthropic-compatible usage land in UnifiedUsage.
	if resp.Response.Usage.CacheReadTokens != 8 || resp.Response.Usage.CacheWriteTokens != 2 {
		t.Fatalf("cache usage = %+v", resp.Response.Usage)
	}
	if gotPath != "/model/anthropic.claude-3-5-haiku-20241022-v1:0/invoke" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody.AnthropicVersion != "bedrock-2023-05-31" {
		t.Fatalf("anthropic_version = %q", gotBody.AnthropicVersion)
	}
	if gotBody.Model != nil {
		t.Fatalf("model must be stripped from the body (URL carries it): %v", gotBody.Model)
	}
	if len(gotBody.Messages) != 1 || gotBody.MaxTokens <= 0 {
		t.Fatalf("body messages/max_tokens lost: %+v", gotBody)
	}
	if !strings.Contains(gotAuth, "Credential=AKIDEXAMPLE/") || !strings.Contains(gotAuth, "/bedrock/aws4_request") {
		t.Fatalf("authorization = %q", gotAuth)
	}
}

func TestBedrockRejectsMissingAWSCredential(t *testing.T) {
	_, connector := newTestBedrock(t, func(w http.ResponseWriter, r *http.Request) {})
	connector.secrets = memorySecrets{"secret://aws": []byte("not-json-at-all")}
	_, err := connector.Invoke(context.Background(), contracts.InvocationRequest{Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "m", Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "hi"}}}}})
	if err == nil {
		t.Fatal("malformed AWS credential must error")
	}
}
