// Package vertex invokes Vertex AI's Anthropic-compatible Claude endpoints
// (publishers/anthropic/models/{model}:rawPredict) authenticating with a
// Google service-account OAuth access token. The wire payload is the Anthropic
// Messages format, so encode/decode reuse the Anthropic protocol adapter.
package vertex

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	protocol "github.com/F31/liteAIG/internal/access/protocol/anthropic"
	"github.com/F31/liteAIG/internal/connectors/model/vertex/vertextoken"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

type SecretResolver interface {
	Resolve(context.Context, string) ([]byte, error)
}

// Config configures one Vertex location+publisher. Credentials carry the
// service-account JSON; access tokens are minted automatically via the
// vertextoken signer. BaseURL overrides the derived
// https://{location}-aiplatform.googleapis.com endpoint (used by tests).
type Config struct {
	Region, ProjectID, SecretRef, Location, BaseURL string
	RequestTimeout, HealthTimeout                   time.Duration
	AllowInsecureHTTP                               bool
	Capabilities                                    contracts.CapabilitySet
	Parameters                                      interaction.ParameterSupport
	TokenClient                                     *http.Client
	Now                                             func() time.Time
}

func DefaultConfig() Config {
	return Config{Region: "us-central1", RequestTimeout: 30 * time.Second, HealthTimeout: 2 * time.Second, Capabilities: contracts.CapabilitySet{"chat": true}}
}

type Connector struct {
	config  Config
	client  *http.Client
	secrets SecretResolver

	mu     sync.Mutex
	signer *vertextoken.Signer
}

func New(config Config, client *http.Client, secrets SecretResolver) (*Connector, error) {
	if config.Location == "" {
		config.Location = config.Region
	}
	// ProjectID may be empty: it is derived from the service-account JSON at
	// request time when the config does not pin it.
	if config.Location == "" || config.SecretRef == "" ||
		config.RequestTimeout <= 0 || config.HealthTimeout <= 0 || client == nil || secrets == nil {
		return nil, errors.New("complete Vertex connector configuration is required")
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://" + url.PathEscape(config.Location) + "-aiplatform.googleapis.com"
	}
	if !strings.HasPrefix(config.BaseURL, "https://") && !config.AllowInsecureHTTP {
		return nil, errors.New("Vertex connector requires HTTPS")
	}
	return &Connector{config: config, client: client, secrets: secrets}, nil
}

// rawPredictPath is the Vertex endpoint for Anthropic-compatible Claude
// models. The publicly available publisher is "anthropic".
func (c *Connector) rawPredictPath(model string) string {
	return "/v1/projects/" + url.PathEscape(c.config.ProjectID) + "/locations/" + url.PathEscape(c.config.Location) + "/publishers/anthropic/models/" + url.PathEscape(model) + ":rawPredict"
}

func (c *Connector) Invoke(ctx context.Context, request contracts.InvocationRequest) (*contracts.InvocationResponse, error) {
	if request.Request == nil || request.Request.Kind != interaction.RequestChat && request.Request.Kind != interaction.RequestResponses {
		return nil, errors.New("Vertex connector requires chat request")
	}
	body, err := c.encode(request.Request)
	if err != nil {
		return nil, err
	}
	response, err := c.do(ctx, c.rawPredictPath(request.Request.Model), body)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, c.statusError(response)
	}
	canonical, err := protocol.DecodeResponse(response.Body)
	if err != nil {
		return nil, err
	}
	return &contracts.InvocationResponse{Response: canonical}, nil
}

func (c *Connector) Stream(ctx context.Context, request contracts.InvocationRequest, writer contracts.StreamWriter) error {
	return errors.New("vertex stream not supported on this connector")
}

func (c *Connector) Health(ctx context.Context, _ contracts.TargetRef) contracts.HealthStatus {
	started := time.Now()
	healthCtx, cancel := context.WithTimeout(ctx, c.config.HealthTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(healthCtx, http.MethodGet, strings.TrimRight(c.config.BaseURL, "/")+"/", nil)
	if err == nil {
		response, callErr := c.client.Do(request)
		err = callErr
		if response != nil {
			response.Body.Close()
		}
	}
	return contracts.HealthStatus{Healthy: err == nil, CheckedAt: started, Reason: errorText(err)}
}

func (c *Connector) Capabilities(context.Context, contracts.TargetRef) contracts.CapabilitySet {
	result := make(contracts.CapabilitySet, len(c.config.Capabilities))
	for k, v := range c.config.Capabilities {
		result[k] = v
	}
	return result
}

func (c *Connector) SupportedParams() interaction.ParameterSupport { return c.config.Parameters }

func (c *Connector) NormalizeError(err error) *contracts.UpstreamError {
	var upstream *contracts.UpstreamError
	if errors.As(err, &upstream) {
		return upstream
	}
	return &contracts.UpstreamError{Code: "upstream_transport_error", Retryable: errors.Is(err, context.DeadlineExceeded), Message: "upstream request failed"}
}

// encode renders the Anthropic Messages payload plus the Vertex-specific
// anthropic_version field.
func (c *Connector) encode(request *interaction.UnifiedRequest) ([]byte, error) {
	body, err := protocol.EncodeMessages(request)
	if err != nil {
		return nil, err
	}
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}
	wire["anthropic_version"] = "vertex-2023-10-16"
	return json.Marshal(wire)
}

func (c *Connector) do(ctx context.Context, path string, body []byte) (*http.Response, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	secret, err := c.secrets.Resolve(callCtx, c.config.SecretRef)
	if err != nil {
		cancel()
		return nil, errors.New("resolve provider credential")
	}
	secretCopy := append([]byte(nil), secret...)
	defer clear(secretCopy)
	account, err := vertextoken.ParseServiceAccount(secretCopy)
	if err != nil {
		cancel()
		return nil, err
	}
	if account.ProjectID != "" && c.config.ProjectID == "" {
		c.config.ProjectID = account.ProjectID
	}
	key, err := vertextoken.ParsePrivateKey([]byte(account.PrivateKey))
	if err != nil {
		cancel()
		return nil, err
	}
	token, err := c.accessToken(account, key)
	if err != nil {
		cancel()
		return nil, err
	}
	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, strings.TrimRight(c.config.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		cancel()
		return nil, err
	}
	response.Body = &cancelBody{ReadCloser: response.Body, cancel: cancel}
	return response, nil
}

// accessToken returns a cached OAuth access token for the service account,
// minting the signer once (guarded against concurrent requests).
func (c *Connector) accessToken(account vertextoken.ServiceAccount, key *rsa.PrivateKey) (string, error) {
	c.mu.Lock()
	if c.signer == nil {
		tokenClient := c.config.TokenClient
		if tokenClient == nil {
			tokenClient = c.client
		}
		c.signer = vertextoken.New(account, key, tokenClient, c.config.Now)
	}
	signer := c.signer
	c.mu.Unlock()
	return signer.AccessToken(context.Background())
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (c *Connector) statusError(response *http.Response) error {
	io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	retryable := response.StatusCode == 429 || response.StatusCode >= 500
	return &contracts.UpstreamError{Code: "upstream_http_error", StatusCode: response.StatusCode, Retryable: retryable, Message: http.StatusText(response.StatusCode)}
}

type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelBody) Close() error { err := b.ReadCloser.Close(); b.cancel(); return err }

var _ contracts.InteractionInvoker = (*Connector)(nil)
