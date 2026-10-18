package guardrail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPProvider evaluates released streaming content through an external
// content-safety service. It is the production implementation of
// ExternalProvider used by the Layer 3 shadow guard.
type HTTPProvider struct {
	client  *http.Client
	url     string
	token   string
	timeout time.Duration
}

// HTTPProviderConfig configures the external guardrail endpoint.
type HTTPProviderConfig struct {
	URL     string
	Token   string
	Timeout time.Duration
	Client  *http.Client
}

// NewHTTPProvider validates the endpoint and builds a provider. The caller is
// expected to pass the same egress-guarded client used by provider connectors.
func NewHTTPProvider(config HTTPProviderConfig) (*HTTPProvider, error) {
	parsed, err := url.Parse(strings.TrimSpace(config.URL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("external guardrail requires an http(s) URL")
	}
	if config.Client == nil {
		return nil, errors.New("external guardrail requires an HTTP client")
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &HTTPProvider{client: config.Client, url: parsed.String(), token: config.Token, timeout: timeout}, nil
}

type externalWireRequest struct {
	TenantID    string `json:"tenant_id"`
	ProjectID   string `json:"project_id"`
	RequestID   string `json:"request_id"`
	ContentHash string `json:"content_hash"`
	Content     string `json:"content"`
}

type externalWireResponse struct {
	Blocked  bool   `json:"blocked"`
	Degraded bool   `json:"degraded,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Evaluate posts the released content and maps the service verdict. A
// non-2xx response, transport failure, or malformed body is an error so the
// caller can apply its configured fail-open/fail-closed policy.
func (p *HTTPProvider) Evaluate(ctx context.Context, request ExternalRequest) (ExternalResult, error) {
	if p == nil {
		return ExternalResult{}, errors.New("external guardrail is not configured")
	}
	callCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	body, err := json.Marshal(externalWireRequest{
		TenantID: request.TenantID, ProjectID: request.ProjectID, RequestID: request.RequestID,
		ContentHash: request.ContentHash, Content: request.Content,
	})
	if err != nil {
		return ExternalResult{}, err
	}
	httpRequest, err := http.NewRequestWithContext(callCtx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return ExternalResult{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if p.token != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+p.token)
	}
	response, err := p.client.Do(httpRequest)
	if err != nil {
		return ExternalResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return ExternalResult{}, fmt.Errorf("external guardrail returned status %d", response.StatusCode)
	}
	var wire externalWireResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&wire); err != nil {
		return ExternalResult{}, err
	}
	return ExternalResult{Blocked: wire.Blocked, Degraded: wire.Degraded, Reason: wire.Reason}, nil
}

var _ ExternalProvider = (*HTTPProvider)(nil)
