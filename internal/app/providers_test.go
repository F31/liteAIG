package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/platform/egress"
	"github.com/F31/liteAIG/internal/platform/secrets"
)

func TestProviderResolverUsesDeploymentUpstreamModel(t *testing.T) {
	var gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotModel = body.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","model":"real-upstream-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
		TenantID: "tenant", TenantRef: "tenant-ref", Version: 1,
		Providers:   []runtime.Provider{{ID: "provider", Type: "openai-compatible", Endpoint: server.URL, Status: "enabled"}},
		Credentials: []runtime.Credential{{ID: "credential", SecretRef: "secret://provider", Status: "enabled"}},
		Deployments: []runtime.Deployment{{ID: "deployment", ProviderID: "provider", CredentialID: "credential", UpstreamModel: "real-upstream-model", Status: "enabled"}},
	})
	deployment, ok := snapshot.Deployment("deployment")
	if !ok {
		t.Fatal("deployment missing")
	}
	resolver := newProviderResolver(server.Client(), secrets.NewMemoryProvider(map[string][]byte{"secret://provider": []byte("provider-secret")}), egress.LitePolicy())
	invoker, ok := resolver.Resolve(snapshot, deployment)
	if !ok {
		t.Fatal("resolve invoker")
	}
	_, err := invoker.Invoke(context.Background(), contracts.InvocationRequest{Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "logical-alias", Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "hello"}}}}})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if gotModel != "real-upstream-model" {
		t.Fatalf("upstream model = %q, want real-upstream-model", gotModel)
	}
}

// TestProviderResolverBuildsNativeProviders verifies the azure-openai, bedrock,
// vertex, and gemini connectors are built from their endpoints with derived
// region/location without invoking upstream (no network call at resolve time).
func TestProviderResolverBuildsNativeProviders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("resolver must not invoke upstream during Resolve (got %s %s)", r.Method, r.URL.Path)
	}))
	defer server.Close()

	cases := []struct {
		name     string
		typ      string
		endpoint string
		secret   string
	}{
		{"azure-openai", "azure-openai", "https://acme-openai.openai.azure.com", "azure-key"},
		{"bedrock", "bedrock", "https://bedrock-runtime.eu-west-1.amazonaws.com", `{"access_key_id":"AKID","secret_access_key":"secret"}`},
		{"vertex", "vertex", "https://europe-west4-aiplatform.googleapis.com", `{"client_email":"sa@proj.iam.gserviceaccount.com","private_key":"x","project_id":"proj"}`},
		{"gemini", "gemini", "https://generativelanguage.googleapis.com", "gemini-key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{
				TenantID: "tenant", TenantRef: "tenant-ref", Version: 1,
				Providers:   []runtime.Provider{{ID: "provider", Type: tc.typ, Endpoint: tc.endpoint, Status: "enabled"}},
				Credentials: []runtime.Credential{{ID: "credential", SecretRef: "secret://provider", Status: "enabled"}},
				Deployments: []runtime.Deployment{{ID: "deployment", ProviderID: "provider", CredentialID: "credential", UpstreamModel: "deployment-name", Status: "enabled"}},
			})
			deployment, ok := snapshot.Deployment("deployment")
			if !ok {
				t.Fatal("deployment missing")
			}
			resolver := newProviderResolver(server.Client(), secrets.NewMemoryProvider(map[string][]byte{"secret://provider": []byte(tc.secret)}), egress.LitePolicy())
			invoker, ok := resolver.Resolve(snapshot, deployment)
			if !ok {
				t.Fatalf("resolve %s invoker failed", tc.typ)
			}
			if invoker == nil {
				t.Fatalf("resolve %s returned nil invoker", tc.typ)
			}
		})
	}
}

// regionFromEndpoint derives the AWS region from a Bedrock Runtime host.
func TestRegionFromEndpoint(t *testing.T) {
	if got := regionFromEndpoint("https://bedrock-runtime.ap-southeast-2.amazonaws.com", "bedrock-runtime"); got != "ap-southeast-2" {
		t.Fatalf("region = %q", got)
	}
	if got := regionFromEndpoint("https://not-a-bedrock.example.com", "bedrock-runtime"); got != "us-east-1" {
		t.Fatalf("fallback region = %q", got)
	}
}

// locationFromEndpoint derives the Vertex location from the aiplatform host.
func TestLocationFromEndpoint(t *testing.T) {
	if got := locationFromEndpoint("https://europe-west4-aiplatform.googleapis.com"); got != "europe-west4" {
		t.Fatalf("location = %q", got)
	}
	if got := locationFromEndpoint("https://example.com"); got != "us-central1" {
		t.Fatalf("fallback location = %q", got)
	}
}
