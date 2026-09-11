package app

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/platform/secrets"
)

// probeFunc probes an upstream provider on demand (may be nil to disable
// probe-driven routing health).
type probeFunc func(ctx context.Context, providerType, endpoint, secretRef string) (healthy bool, reason string)

// probeOutcome is one cached upstream reachability result.
type probeOutcome struct {
	healthy bool
	reason  string
	at      time.Time
}

// providerProber performs real model-catalog probes against enabled upstream
// providers on demand (Health page), caching results briefly so refreshes do
// not hammer every provider. Probes go through the same egress-limited client
// as the data plane, so SSRF policy applies identically.
type providerProber struct {
	client  *http.Client
	secrets secrets.Provider
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]probeOutcome
}

func newProviderProber(client *http.Client, secretsProvider secrets.Provider) *providerProber {
	return &providerProber{client: client, secrets: secretsProvider, now: time.Now, cache: map[string]probeOutcome{}}
}

const providerProbeTTL = 20 * time.Second

// Probe verifies that the provider's model catalog is reachable and answers
// with the resolved credential. The result is cached for providerProbeTTL.
func (p *providerProber) Probe(ctx context.Context, providerType, endpoint, secretRef string) (healthy bool, reason string) {
	cacheKey := providerType + "|" + endpoint + "|" + secretRef
	p.mu.Lock()
	cached, ok := p.cache[cacheKey]
	if ok && p.now().Sub(cached.at) < providerProbeTTL {
		p.mu.Unlock()
		return cached.healthy, cached.reason
	}
	p.mu.Unlock()

	healthy, reason = p.doProbe(ctx, providerType, endpoint, secretRef)

	p.mu.Lock()
	if len(p.cache) > 64 {
		// Bound the cache; entries are keyed by credential ref and rotate rarely.
		p.cache = map[string]probeOutcome{}
	}
	p.cache[cacheKey] = probeOutcome{healthy: healthy, reason: reason, at: p.now()}
	p.mu.Unlock()
	return healthy, reason
}

func (p *providerProber) doProbe(ctx context.Context, providerType, endpoint, secretRef string) (bool, string) {
	if endpoint == "" || secretRef == "" {
		return false, "no_credential"
	}
	secret, err := p.secrets.Resolve(ctx, secretRef)
	if err != nil || len(secret) == 0 {
		return false, "credential_unavailable"
	}
	defer clearBytes(secret)
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, probeURL(providerType, endpoint), nil)
	if err != nil {
		return false, "invalid_endpoint"
	}
	switch providerType {
	case "anthropic":
		request.Header.Set("x-api-key", string(secret))
		request.Header.Set("anthropic-version", "2023-06-01")
	case "azure-openai":
		request.Header.Set("api-key", string(secret))
	case "gemini":
		request.Header.Set("x-goog-api-key", string(secret))
	default:
		// OpenAI-compatible, Bedrock, and Vertex use the Bearer form for the
		// reachability probe (Vertex/Bedrock health is a shallow reachability
		// check; auth failures still surface as credential_rejected).
		request.Header.Set("Authorization", "Bearer "+string(secret))
	}
	response, err := p.client.Do(request)
	if err != nil {
		return false, "unreachable"
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 400:
		return true, ""
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return false, "credential_rejected"
	case response.StatusCode >= 500:
		return false, "upstream_unavailable"
	default:
		return false, "upstream_error"
	}
}

// probeURL selects the provider's model-catalog probe path. OpenAI-compatible,
// Anthropic, Bedrock, and Vertex share /v1/models; Azure OpenAI scopes the
// catalog under /openai/models with the api-version query; Gemini uses its
// native /v1beta/models catalog.
func probeURL(providerType, endpoint string) string {
	base := strings.TrimRight(endpoint, "/")
	if providerType == "azure-openai" {
		return base + "/openai/models?api-version=2024-06-01"
	}
	if providerType == "gemini" {
		return base + "/v1beta/models"
	}
	return base + "/v1/models"
}
