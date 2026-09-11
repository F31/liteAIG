// Package openai invokes OpenAI and OpenAI-compatible model endpoints.
package openai

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
type Config struct {
	BaseURL, SecretRef, ChatPath, EmbeddingPath, RerankPath, AudioTranscriptionsPath, BatchPath, FilesPath, ModelsPath string
	RequestTimeout, HealthTimeout                                                                                      time.Duration
	AllowInsecureHTTP                                                                                                  bool
	Capabilities                                                                                                       contracts.CapabilitySet
	Parameters                                                                                                         interaction.ParameterSupport
}

func DefaultConfig() Config {
	return Config{ChatPath: "/v1/chat/completions", EmbeddingPath: "/v1/embeddings", RerankPath: "/v1/rerank", AudioTranscriptionsPath: "/v1/audio/transcriptions", BatchPath: "/v1/batches", FilesPath: "/v1/files", ModelsPath: "/v1/models", RequestTimeout: 30 * time.Second, HealthTimeout: 2 * time.Second, Capabilities: contracts.CapabilitySet{"chat": true, "embedding": true, "rerank": true, "audio": true, "batch": true, "stream": true}}
}

type Connector struct {
	config  Config
	client  *http.Client
	secrets SecretResolver
}

func New(config Config, client *http.Client, secrets SecretResolver) (*Connector, error) {
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || parsed.Host == "" {
		return nil, errors.New("valid OpenAI base URL is required")
	}
	if parsed.Scheme != "https" && !config.AllowInsecureHTTP {
		return nil, errors.New("OpenAI connector requires HTTPS")
	}
	if config.SecretRef == "" || config.ChatPath == "" || config.EmbeddingPath == "" || config.RerankPath == "" || config.AudioTranscriptionsPath == "" || config.BatchPath == "" || config.FilesPath == "" || config.ModelsPath == "" || config.RequestTimeout <= 0 || config.HealthTimeout <= 0 || client == nil || secrets == nil {
		return nil, errors.New("complete OpenAI connector configuration is required")
	}
	return &Connector{config: config, client: client, secrets: secrets}, nil
}
func NewCompatible(config Config, client *http.Client, secrets SecretResolver) (*Connector, error) {
	return New(config, client, secrets)
}

func (c *Connector) Invoke(ctx context.Context, request contracts.InvocationRequest) (*contracts.InvocationResponse, error) {
	body, method, path, contentType, err := c.encode(request.Request)
	if err != nil {
		return nil, err
	}
	response, err := c.do(ctx, method, path, body, contentType, false)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, c.statusError(response)
	}
	var canonical *interaction.UnifiedResponse
	switch request.Request.Kind {
	case interaction.RequestChat, interaction.RequestResponses:
		canonical, err = protocol.DecodeChatResponse(response.Body)
	case interaction.RequestEmbedding:
		canonical, err = protocol.DecodeEmbeddingResponse(response.Body)
	case interaction.RequestRerank:
		canonical, err = protocol.DecodeRerankResponse(response.Body)
	case interaction.RequestAudio:
		canonical, err = protocol.DecodeAudioTranscriptionResponse(response.Body)
	case interaction.RequestBatch:
		canonical, err = protocol.DecodeBatchResponse(response.Body)
	case interaction.RequestFile:
		if request.Request.File != nil && (request.Request.File.Operation == "" || request.Request.File.Operation == "content") {
			canonical, err = protocol.DecodeFileContentResponse(response.Body)
		} else {
			canonical, err = protocol.DecodeFileResponse(response.Body)
		}
	default:
		err = errors.New("unsupported OpenAI request kind")
	}
	if err != nil {
		return nil, err
	}
	return &contracts.InvocationResponse{Response: canonical}, nil
}
func (c *Connector) Stream(ctx context.Context, request contracts.InvocationRequest, writer contracts.StreamWriter) error {
	if request.Request == nil || request.Request.Kind != interaction.RequestChat && request.Request.Kind != interaction.RequestResponses {
		return errors.New("OpenAI stream requires chat request")
	}
	streamRequest := *request.Request
	streamRequest.Stream = true
	body, method, path, contentType, err := c.encode(&streamRequest)
	if err != nil {
		return err
	}
	response, err := c.do(ctx, method, path, body, contentType, true)
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
	request, err := http.NewRequestWithContext(healthCtx, http.MethodGet, strings.TrimRight(c.config.BaseURL, "/")+c.config.ModelsPath, nil)
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
	return cloneCapabilities(c.config.Capabilities)
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
func (c *Connector) encode(request *interaction.UnifiedRequest) ([]byte, string, string, string, error) {
	if request == nil {
		return nil, "", "", "", errors.New("request is required")
	}
	switch request.Kind {
	case interaction.RequestChat, interaction.RequestResponses:
		body, err := protocol.EncodeChat(request)
		return body, http.MethodPost, c.config.ChatPath, "application/json", err
	case interaction.RequestEmbedding:
		body, err := protocol.EncodeEmbedding(request)
		return body, http.MethodPost, c.config.EmbeddingPath, "application/json", err
	case interaction.RequestRerank:
		body, err := protocol.EncodeRerank(request)
		return body, http.MethodPost, c.config.RerankPath, "application/json", err
	case interaction.RequestAudio:
		body, contentType, err := protocol.EncodeAudioTranscription(request)
		return body, http.MethodPost, c.config.AudioTranscriptionsPath, contentType, err
	case interaction.RequestBatch:
		return c.encodeBatch(request)
	case interaction.RequestFile:
		return c.encodeFile(request)
	default:
		return nil, "", "", "", errors.New("unsupported OpenAI request kind")
	}
}

func (c *Connector) encodeFile(request *interaction.UnifiedRequest) ([]byte, string, string, string, error) {
	if request.File == nil {
		return nil, "", "", "", errors.New("file payload is required")
	}
	base := strings.TrimRight(c.config.FilesPath, "/")
	switch request.File.Operation {
	case "upload":
		body, contentType, err := protocol.EncodeFileUpload(request)
		return body, http.MethodPost, base, contentType, err
	case "retrieve":
		if request.File.ID == "" {
			return nil, "", "", "", errors.New("file id is required")
		}
		return nil, http.MethodGet, base + "/" + url.PathEscape(request.File.ID), "application/json", nil
	case "delete":
		if request.File.ID == "" {
			return nil, "", "", "", errors.New("file id is required")
		}
		return nil, http.MethodDelete, base + "/" + url.PathEscape(request.File.ID), "application/json", nil
	case "", "content":
		if request.File.ID == "" {
			return nil, "", "", "", errors.New("file id is required")
		}
		return nil, http.MethodGet, base + "/" + url.PathEscape(request.File.ID) + "/content", "application/octet-stream", nil
	case "list":
		values := url.Values{}
		if request.File.Limit > 0 {
			values.Set("limit", fmt.Sprintf("%d", request.File.Limit))
		}
		if request.File.After != "" {
			values.Set("after", request.File.After)
		}
		path := base
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return nil, http.MethodGet, path, "application/json", nil
	default:
		return nil, "", "", "", errors.New("unsupported file operation")
	}
}

func (c *Connector) encodeBatch(request *interaction.UnifiedRequest) ([]byte, string, string, string, error) {
	if request.Batch == nil {
		return nil, "", "", "", errors.New("batch payload is required")
	}
	switch request.Batch.Operation {
	case "", "create":
		body, err := protocol.EncodeBatchCreate(request)
		return body, http.MethodPost, c.config.BatchPath, "application/json", err
	case "retrieve":
		return nil, http.MethodGet, c.config.BatchPath + "/" + url.PathEscape(request.Batch.ID), "application/json", nil
	case "cancel":
		return nil, http.MethodPost, c.config.BatchPath + "/" + url.PathEscape(request.Batch.ID) + "/cancel", "application/json", nil
	case "list":
		values := url.Values{}
		if request.Batch.Limit > 0 {
			values.Set("limit", fmt.Sprintf("%d", request.Batch.Limit))
		}
		if request.Batch.After != "" {
			values.Set("after", request.Batch.After)
		}
		path := c.config.BatchPath
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return nil, http.MethodGet, path, "application/json", nil
	default:
		return nil, "", "", "", errors.New("unsupported batch operation")
	}
}

func (c *Connector) do(ctx context.Context, method, path string, body []byte, contentType string, stream bool) (*http.Response, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer func() {
		if callCtx.Err() != nil {
			cancel()
		}
	}()
	secret, err := c.secrets.Resolve(callCtx, c.config.SecretRef)
	if err != nil {
		cancel()
		return nil, errors.New("resolve provider credential")
	}
	secretCopy := append([]byte(nil), secret...)
	defer clear(secretCopy)
	request, err := http.NewRequestWithContext(callCtx, method, strings.TrimRight(c.config.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+string(secretCopy))
	request.Header.Set("Content-Type", contentType)
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
func cloneCapabilities(input contracts.CapabilitySet) contracts.CapabilitySet {
	result := make(contracts.CapabilitySet, len(input))
	for k, v := range input {
		result[k] = v
	}
	return result
}
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

var _ contracts.InteractionInvoker = (*Connector)(nil)
