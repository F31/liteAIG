// Package azure invokes Azure OpenAI deployments with the Azure resource
// token (api-key header). The wire protocol is OpenAI chat, so encode/decode
// reuse the OpenAI protocol package; the difference is auth (api-key instead
// of Bearer) and the deployment-scoped URL path.
package azure

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	protocol "github.com/F31/liteAIG/internal/access/protocol/openai"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

type SecretResolver interface {
	Resolve(context.Context, string) ([]byte, error)
}

// Config configures one Azure OpenAI resource. BaseURL is the resource
// endpoint (https://{resource}.openai.azure.com). The deployment is taken from
// each request's model (the deployment name configured as UpstreamModel), so a
// single resource can carry multiple deployments.
type Config struct {
	BaseURL, SecretRef, APIVersion string
	RequestTimeout, HealthTimeout  time.Duration
	AllowInsecureHTTP              bool
	Capabilities                   contracts.CapabilitySet
	Parameters                     interaction.ParameterSupport
}

func DefaultConfig() Config {
	return Config{APIVersion: "2024-06-01", RequestTimeout: 30 * time.Second, HealthTimeout: 2 * time.Second, Capabilities: contracts.CapabilitySet{"chat": true, "stream": true}}
}

type Connector struct {
	config  Config
	client  *http.Client
	secrets SecretResolver
}

func New(config Config, client *http.Client, secrets SecretResolver) (*Connector, error) {
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || parsed.Host == "" {
		return nil, errors.New("valid Azure OpenAI base URL is required")
	}
	if parsed.Scheme != "https" && !config.AllowInsecureHTTP {
		return nil, errors.New("Azure OpenAI connector requires HTTPS")
	}
	if config.SecretRef == "" || config.APIVersion == "" || config.RequestTimeout <= 0 || config.HealthTimeout <= 0 || client == nil || secrets == nil {
		return nil, errors.New("complete Azure OpenAI connector configuration is required")
	}
	return &Connector{config: config, client: client, secrets: secrets}, nil
}

// chatPath builds the deployment-scoped Azure chat path. The deployment name
// is the request model (Azure requires the deployment in the URL, not just the
// body).
func (c *Connector) chatPath(model string) string {
	deployment := strings.TrimSpace(model)
	if deployment == "" {
		deployment = "default"
	}
	return "/openai/deployments/" + url.PathEscape(deployment) + "/chat/completions?api-version=" + url.QueryEscape(c.config.APIVersion)
}

func (c *Connector) Invoke(ctx context.Context, request contracts.InvocationRequest) (*contracts.InvocationResponse, error) {
	if request.Request == nil || request.Request.Kind != interaction.RequestChat && request.Request.Kind != interaction.RequestResponses {
		return nil, errors.New("Azure OpenAI connector requires chat request")
	}
	body, err := protocol.EncodeChat(request.Request)
	if err != nil {
		return nil, err
	}
	response, err := c.do(ctx, c.chatPath(request.Request.Model), body, false)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, c.statusError(response)
	}
	canonical, err := protocol.DecodeChatResponse(response.Body)
	if err != nil {
		return nil, err
	}
	return &contracts.InvocationResponse{Response: canonical}, nil
}

func (c *Connector) Stream(ctx context.Context, request contracts.InvocationRequest, writer contracts.StreamWriter) error {
	if request.Request == nil || request.Request.Kind != interaction.RequestChat && request.Request.Kind != interaction.RequestResponses {
		return errors.New("Azure OpenAI stream requires chat request")
	}
	streamRequest := *request.Request
	streamRequest.Stream = true
	body, err := protocol.EncodeChat(&streamRequest)
	if err != nil {
		return err
	}
	response, err := c.do(ctx, c.chatPath(request.Request.Model), body, true)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return c.statusError(response)
	}
	scanner := bufio.NewScanner(response.Body)
	done := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			done = true
			break
		}
		event, err := protocol.NormalizeStream([]byte(data))
		if err != nil {
			return err
		}
		if err := writer.WriteChunk(ctx, contracts.StreamChunk{Event: event}); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !done {
		return io.ErrUnexpectedEOF
	}
	return writer.WriteChunk(ctx, contracts.StreamChunk{Event: interaction.StreamEvent{Final: true}})
}

func (c *Connector) Health(ctx context.Context, _ contracts.TargetRef) contracts.HealthStatus {
	started := time.Now()
	healthCtx, cancel := context.WithTimeout(ctx, c.config.HealthTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(healthCtx, http.MethodGet, strings.TrimRight(c.config.BaseURL, "/")+"/openai/models?api-version="+url.QueryEscape(c.config.APIVersion), nil)
	if err == nil {
		response, callErr := c.client.Do(request)
		err = callErr
		if response != nil {
			response.Body.Close()
			if response.StatusCode >= 500 {
				err = fmt.Errorf("status %d", response.StatusCode)
			}
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
	retryable := errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrUnexpectedEOF)
	return &contracts.UpstreamError{Code: "upstream_transport_error", Retryable: retryable, Message: "upstream request failed"}
}

func (c *Connector) do(ctx context.Context, path string, body []byte, stream bool) (*http.Response, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	secret, err := c.secrets.Resolve(callCtx, c.config.SecretRef)
	if err != nil {
		cancel()
		return nil, errors.New("resolve provider credential")
	}
	secretCopy := append([]byte(nil), secret...)
	defer clear(secretCopy)
	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, strings.TrimRight(c.config.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, err
	}
	request.Header.Set("api-key", string(secretCopy))
	request.Header.Set("Content-Type", "application/json")
	if stream {
		request.Header.Set("Accept", "text/event-stream")
	}
	response, err := c.client.Do(request)
	if err != nil {
		cancel()
		return nil, err
	}
	response.Body = &cancelBody{ReadCloser: response.Body, cancel: cancel}
	return response, nil
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

var _ contracts.InteractionInvoker = (*Connector)(nil)
