// Package bedrock invokes AWS Bedrock Runtime model APIs with the
// Anthropic-compatible Messages wire shape, signing requests with AWS SigV4.
// The credential secret is a JSON object carrying the access key material.
package bedrock

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	protocol "github.com/F31/liteAIG/internal/access/protocol/anthropic"
	"github.com/F31/liteAIG/internal/connectors/model/bedrock/sigv4"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

type SecretResolver interface {
	Resolve(context.Context, string) ([]byte, error)
}

// Config configures one Bedrock Runtime region. BaseURL is optional; when empty
// it is derived from Region as https://bedrock-runtime.{region}.amazonaws.com.
type Config struct {
	BaseURL, Region, SecretRef    string
	RequestTimeout, HealthTimeout time.Duration
	AllowInsecureHTTP             bool
	Capabilities                  contracts.CapabilitySet
	Parameters                    interaction.ParameterSupport
}

func DefaultConfig() Config {
	return Config{RequestTimeout: 30 * time.Second, HealthTimeout: 2 * time.Second, Capabilities: contracts.CapabilitySet{"chat": true}}
}

type Connector struct {
	config  Config
	client  *http.Client
	secrets SecretResolver
	clock   func() time.Time
}

func New(config Config, client *http.Client, secrets SecretResolver) (*Connector, error) {
	if config.Region == "" || config.SecretRef == "" || config.RequestTimeout <= 0 || config.HealthTimeout <= 0 || client == nil || secrets == nil {
		return nil, errors.New("complete Bedrock connector configuration is required")
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://bedrock-runtime." + config.Region + ".amazonaws.com"
	}
	if !strings.HasPrefix(config.BaseURL, "https://") && !config.AllowInsecureHTTP {
		return nil, errors.New("Bedrock connector requires HTTPS")
	}
	return &Connector{config: config, client: client, secrets: secrets, clock: time.Now}, nil
}

// invokePath is the Bedrock InvokeModel path for the Anthropic-compatible
// payload. The model ID (deployment UpstreamModel) is carried in the URL.
func (c *Connector) invokePath(model string) string {
	return "/model/" + model + "/invoke"
}

func (c *Connector) Invoke(ctx context.Context, request contracts.InvocationRequest) (*contracts.InvocationResponse, error) {
	if request.Request == nil || request.Request.Kind != interaction.RequestChat && request.Request.Kind != interaction.RequestResponses {
		return nil, errors.New("Bedrock connector requires chat request")
	}
	body, err := c.encode(request.Request)
	if err != nil {
		return nil, err
	}
	response, err := c.do(ctx, c.invokePath(request.Request.Model), body)
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
	return fmt.Errorf("bedrock stream not supported on this connector")
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

// encode renders the Anthropic-compatible Bedrock payload: the Anthropic
// Messages body plus the required anthropic_version field. request.Model is
// left in the URL path (Bedrock rejects model in the body).
func (c *Connector) encode(request *interaction.UnifiedRequest) ([]byte, error) {
	body, err := protocol.EncodeMessages(request)
	if err != nil {
		return nil, err
	}
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}
	delete(wire, "model")
	wire["anthropic_version"] = "bedrock-2023-05-31"
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
	credentials, err := parseCredentials(secretCopy)
	if err != nil {
		cancel()
		return nil, err
	}
	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, strings.TrimRight(c.config.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, err
	}
	payloadHash := hexSHA256(body)
	request.Header.Set("Content-Type", "application/json")
	signer := sigv4.New(credentials, "bedrock", c.config.Region, c.clock)
	signer.Sign(request, payloadHash, time.Time{})
	response, err := c.client.Do(request)
	if err != nil {
		cancel()
		return nil, err
	}
	response.Body = &cancelBody{ReadCloser: response.Body, cancel: cancel}
	return response, nil
}

// awsCredentialJSON is the accepted secret shape for a Bedrock credential.
type awsCredentialJSON struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
}

func parseCredentials(data []byte) (sigv4.Credentials, error) {
	var credential awsCredentialJSON
	if err := json.Unmarshal(data, &credential); err != nil {
		return sigv4.Credentials{}, errors.New("Bedrock credential must be JSON ({access_key_id, secret_access_key, session_token})")
	}
	if credential.AccessKeyID == "" || credential.SecretAccessKey == "" {
		return sigv4.Credentials{}, errors.New("Bedrock credential requires access_key_id and secret_access_key")
	}
	return sigv4.Credentials{AccessKeyID: credential.AccessKeyID, SecretAccessKey: credential.SecretAccessKey, SessionToken: credential.SessionToken}, nil
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
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func hexSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

var _ contracts.InteractionInvoker = (*Connector)(nil)
