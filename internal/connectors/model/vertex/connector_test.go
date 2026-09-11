package vertex

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

type memorySecrets map[string][]byte

func (m memorySecrets) Resolve(_ context.Context, ref string) ([]byte, error) {
	return m[ref], nil
}

// testKeyPEM produces a fresh RSA key for the service-account secret.
func testKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// newTestVertex wires a mock token endpoint + mock Vertex AI endpoint. The
// connector's BaseURL points at the mock Vertex server.
func newTestVertex(t *testing.T, vertexHandler http.HandlerFunc) (*httptest.Server, *Connector) {
	t.Helper()
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ya29.vertex-token","expires_in":3600}`))
	}))
	t.Cleanup(tokenServer.Close)

	vertexServer := httptest.NewServer(vertexHandler)
	t.Cleanup(vertexServer.Close)

	secret := fmt.Sprintf(`{"client_email":"sa@proj.iam.gserviceaccount.com","private_key":%q,"project_id":"proj","token_uri":%q}`, testKeyPEM(t), tokenServer.URL)
	connector, err := New(Config{Location: "us-central1", ProjectID: "proj", SecretRef: "secret://sa", BaseURL: vertexServer.URL, TokenClient: tokenServer.Client(), AllowInsecureHTTP: true, RequestTimeout: 5e9, HealthTimeout: 1e9, Capabilities: contracts.CapabilitySet{"chat": true}}, vertexServer.Client(), memorySecrets{"secret://sa": []byte(secret)})
	if err != nil {
		t.Fatal(err)
	}
	return vertexServer, connector
}

func TestVertexInvokeAnthropicCompatible(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	_, connector := newTestVertex(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotAuth = r.Header.Get("Authorization")
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg-vtx","type":"message","role":"assistant","model":"claude-3-5-haiku","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":20,"output_tokens":2,"cache_read_input_tokens":15}}`))
	})
	resp, err := connector.Invoke(context.Background(), contracts.InvocationRequest{Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "claude-3-5-haiku@20241022", Chat: &interaction.ChatPayload{MaxOutputTokens: intPtr(16), Messages: []interaction.Message{{Role: "user", Content: "hi"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Response.Choices[0].Message.Content != "ok" {
		t.Fatalf("response = %+v", resp.Response)
	}
	if resp.Response.Usage.CacheReadTokens != 15 {
		t.Fatalf("cache usage = %+v", resp.Response.Usage)
	}
	if !strings.Contains(gotPath, "/v1/projects/proj/locations/us-central1/publishers/anthropic/models/claude-3-5-haiku@20241022:rawPredict") {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer ya29.vertex-token" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if !strings.Contains(gotBody, `"anthropic_version":"vertex-2023-10-16"`) {
		t.Fatalf("body missing anthropic_version: %s", gotBody)
	}
}

func TestVertexMalformedCredentialFails(t *testing.T) {
	_, connector := newTestVertex(t, func(w http.ResponseWriter, r *http.Request) {})
	connector.secrets = memorySecrets{"secret://sa": []byte("not-a-service-account")}
	_, err := connector.Invoke(context.Background(), contracts.InvocationRequest{Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "m", Chat: &interaction.ChatPayload{MaxOutputTokens: intPtr(16), Messages: []interaction.Message{{Role: "user", Content: "hi"}}}}})
	if err == nil {
		t.Fatal("malformed service account must error")
	}
}

func intPtr(value int) *int { return &value }

var _ = time.Second
