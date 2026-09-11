package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/F31/liteAIG/internal/platform/secrets"
)

// staticSecretProvider resolves every ref to a fixed credential.
type staticSecretProvider struct{ value []byte }

func (s staticSecretProvider) Resolve(context.Context, string) ([]byte, error) {
	return s.value, nil
}

var _ secrets.Provider = staticSecretProvider{}

func TestProviderProberClassifiesProbeOutcomes(t *testing.T) {
	ctx := context.Background()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer provider.Close()

	prober := newProviderProber(provider.Client(), staticSecretProvider{value: []byte("sk-good")})
	healthy, reason := prober.Probe(ctx, "openai", provider.URL, "local://credential/good")
	if !healthy || reason != "" {
		t.Fatalf("healthy probe = %v %q", healthy, reason)
	}

	prober = newProviderProber(provider.Client(), staticSecretProvider{value: []byte("sk-bad")})
	healthy, reason = prober.Probe(ctx, "openai", provider.URL, "local://credential/bad")
	if healthy || reason != "credential_rejected" {
		t.Fatalf("rejected probe = %v %q, want credential_rejected", healthy, reason)
	}
}

func TestProviderProberRequiresResolvableCredential(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer provider.Close()
	prober := newProviderProber(provider.Client(), staticSecretProvider{value: nil})
	healthy, reason := prober.Probe(context.Background(), "openai", provider.URL, "local://credential/x")
	if healthy || reason != "credential_unavailable" {
		t.Fatalf("probe = %v %q, want credential_unavailable", healthy, reason)
	}
}
