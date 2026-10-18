package guardrail

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPProviderEvaluatesContent(t *testing.T) {
	var seen externalWireRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Error("missing external guardrail bearer token")
		}
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(externalWireResponse{Blocked: true, Reason: "unsafe"})
	}))
	defer server.Close()
	provider, err := NewHTTPProvider(HTTPProviderConfig{URL: server.URL, Token: "token", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.Evaluate(context.Background(), ExternalRequest{
		TenantID: "tenant", ProjectID: "project", RequestID: "request", ContentHash: "hash", Content: "released text",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Blocked || result.Reason != "unsafe" {
		t.Fatalf("result = %+v", result)
	}
	if seen.Content != "released text" || seen.ContentHash != "hash" || seen.TenantID != "tenant" {
		t.Fatalf("wire request = %+v", seen)
	}
}

func TestHTTPProviderFailsOnBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	provider, err := NewHTTPProvider(HTTPProviderConfig{URL: server.URL, Client: server.Client(), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Evaluate(context.Background(), ExternalRequest{}); err == nil {
		t.Fatal("expected an error for a non-2xx external guardrail response")
	}
}

func TestHTTPProviderRejectsInvalidURL(t *testing.T) {
	if _, err := NewHTTPProvider(HTTPProviderConfig{URL: "file:///tmp/x", Client: http.DefaultClient}); err == nil {
		t.Fatal("expected invalid scheme to be rejected")
	}
}
