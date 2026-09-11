// Package a2a invokes remote agents over the A2A 1.0 protocol.
package a2a

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	wirea2a "github.com/F31/liteAIG/internal/access/protocol/a2a"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

type SecretResolver interface {
	Resolve(context.Context, string) ([]byte, error)
}

type Config struct {
	BaseURL string
	Timeout time.Duration
	// SecretRef is the outbound credential reference resolved through secrets.
	// Empty keeps the endpoint unauthenticated. AuthScheme selects how the
	// resolved material is attached: "bearer" -> Authorization: Bearer <s>,
	// "x-api-key" -> x-api-key: <s>.
	SecretRef  string
	AuthScheme string
	// Headers are extra static outbound headers merged onto the request.
	Headers map[string]string
}

// Connector invokes a remote A2A agent endpoint. It never makes governance
// decisions; trust, grants, budget, and task limits are enforced upstream.
type Connector struct {
	config  Config
	client  *http.Client
	secrets SecretResolver
	now     func() time.Time
}

func New(config Config, client *http.Client, secrets SecretResolver) (*Connector, error) {
	if config.BaseURL == "" || config.Timeout <= 0 || client == nil {
		return nil, errors.New("complete A2A connector configuration is required")
	}
	if config.SecretRef != "" && secrets == nil {
		return nil, errors.New("A2A connector requires a secret resolver when a secret ref is configured")
	}
	return &Connector{config: config, client: client, secrets: secrets, now: time.Now}, nil
}

// wireEnvelope is the A2A JSON-RPC message/send request. A2A message/send is
// not idempotent, so callers must never auto-retry a send after an ambiguous
// failure (this connector therefore reports upstream failures as non-retryable).
type wireEnvelope struct {
	JSONRPC string     `json:"jsonrpc"`
	ID      string     `json:"id"`
	Method  string     `json:"method"`
	Params  wireParams `json:"params"`
}

type wireParams struct {
	Message wireMessage `json:"message"`
}

type wireMessage struct {
	MessageID string     `json:"messageId"`
	Role      string     `json:"role"`
	Parts     []wirePart `json:"parts"`
}

type wirePart struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// send builds and performs one conformant message/send POST. When stream is
// true the request advertises the LiteAIG A2A SSE streaming profile via
// Accept: text/event-stream. The caller owns the returned body and MUST invoke
// the returned cancel after reading it so the bounded timeout context is
// released. Upstream 4xx/5xx are surfaced as non-retryable errors with the
// body already drained.
func (c *Connector) send(ctx context.Context, request contracts.InvocationRequest, stream bool) (*http.Response, func(), error) {
	if request.Request == nil || request.Request.Chat == nil {
		return nil, func() {}, errors.New("A2A message is required")
	}
	role, text := chatTurn(request.Request.Chat)
	messageID := request.RequestID
	if messageID == "" {
		messageID = c.now().UTC().Format("20060102T150405.000000000Z")
	}
	payload, err := json.Marshal(wireEnvelope{
		JSONRPC: "2.0",
		ID:      messageID,
		Method:  "message/send",
		Params: wireParams{Message: wireMessage{
			MessageID: messageID,
			Role:      role,
			Parts:     []wirePart{{Kind: "text", Text: text}},
		}},
	})
	if err != nil {
		return nil, func() {}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	httpRequest, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.config.BaseURL, bytes.NewReader(payload))
	if err != nil {
		cancel()
		return nil, func() {}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("A2A-Protocol-Version", "1.0.0")
	if stream {
		httpRequest.Header.Set("Accept", "text/event-stream")
	}
	if err := c.attachCredentials(callCtx, httpRequest); err != nil {
		cancel()
		return nil, func() {}, err
	}
	response, err := c.client.Do(httpRequest)
	if err != nil {
		cancel()
		return nil, func() {}, err
	}
	if response.StatusCode >= 400 {
		response.Body.Close()
		cancel()
		// A message/send is never retried after a 4xx/5xx or timeout: the peer
		// may already have accepted and acted on it.
		return nil, func() {}, &contracts.UpstreamError{Code: "A2A_UPSTREAM_ERROR", StatusCode: response.StatusCode, Retryable: false, Message: "A2A invocation failed"}
	}
	return response, cancel, nil
}

// resultWire is the blocking JSON message/send result shape.
type resultWire struct {
	Result struct {
		Message struct {
			Parts []struct {
				Kind string `json:"kind"`
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"message"`
	} `json:"result"`
	Error *struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// decodeResult turns a blocking JSON message/send result into the canonical
// tool result. A JSON-RPC error object surfaces as a non-retryable upstream
// error (message/send is never retried).
func decodeResult(body io.Reader, request contracts.InvocationRequest, statusCode int) (*interaction.UnifiedResponse, error) {
	var wire resultWire
	if err := json.NewDecoder(body).Decode(&wire); err != nil {
		return nil, err
	}
	if wire.Error != nil {
		message := wire.Error.Message
		if message == "" {
			message = "A2A invocation failed"
		}
		return nil, &contracts.UpstreamError{Code: "A2A_RPC_ERROR", StatusCode: statusCode, Retryable: false, Message: message}
	}
	var content strings.Builder
	for _, part := range wire.Result.Message.Parts {
		if part.Kind == "text" {
			if content.Len() > 0 {
				content.WriteString("\n")
			}
			content.WriteString(part.Text)
		}
	}
	return &interaction.UnifiedResponse{ID: request.RequestID, ToolResult: &interaction.ToolResult{
		Name:       request.Target.ID,
		Content:    content.String(),
		Provenance: interaction.Provenance{Source: "external_agent_response", Trusted: false},
	}}, nil
}

func (c *Connector) Invoke(ctx context.Context, request contracts.InvocationRequest) (*contracts.InvocationResponse, error) {
	response, cancel, err := c.send(ctx, request, false)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	defer cancel()
	parsed, err := decodeResult(response.Body, request, response.StatusCode)
	if err != nil {
		return nil, err
	}
	return &contracts.InvocationResponse{Response: parsed}, nil
}

// Stream writes the agent reply to the writer as normalized stream events.
// When the caller opted into streaming (Request.Stream) the connector requests
// the LiteAIG A2A SSE streaming profile and forwards one event per text delta,
// terminating on the completed event (a peer that answers with plain JSON is
// still honored as a single final chunk). Blocking requests keep the historical
// behavior: one final chunk carrying the whole reply. Streaming consumption is
// bounded by the same Config.Timeout as blocking calls.
func (c *Connector) Stream(ctx context.Context, request contracts.InvocationRequest, writer contracts.StreamWriter) error {
	if request.Request == nil {
		return errors.New("A2A message is required")
	}
	stream := request.Request.Stream
	response, cancel, err := c.send(ctx, request, stream)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	defer cancel()
	if stream && strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		return c.consumeSSE(ctx, response.Body, request, writer)
	}
	parsed, err := decodeResult(response.Body, request, response.StatusCode)
	if err != nil {
		return err
	}
	content := ""
	if parsed.ToolResult != nil {
		content = parsed.ToolResult.Content
	}
	return writer.WriteChunk(ctx, contracts.StreamChunk{Event: interaction.StreamEvent{ID: parsed.ID, Delta: content, Final: true}})
}

// consumeSSE reads the LiteAIG A2A SSE streaming profile from the peer body:
// one canonical data frame per line. Each message frame is forwarded as one
// delta chunk; a completed frame (or a peer [DONE] marker) forwards the final
// chunk. Comment/keep-alive/event lines are ignored. Ending without a terminal
// frame is a stream error so a truncated peer never looks successful.
func (c *Connector) consumeSSE(ctx context.Context, body io.Reader, request contracts.InvocationRequest, writer contracts.StreamWriter) error {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			return writer.WriteChunk(ctx, contracts.StreamChunk{Event: interaction.StreamEvent{ID: request.RequestID, Final: true}})
		}
		delta, completed, err := wirea2a.ParseStreamFrame([]byte(data))
		if err != nil {
			return err
		}
		if delta != "" {
			if err := writer.WriteChunk(ctx, contracts.StreamChunk{Event: interaction.StreamEvent{ID: request.RequestID, Delta: delta}}); err != nil {
				return err
			}
		}
		if completed {
			return writer.WriteChunk(ctx, contracts.StreamChunk{Event: interaction.StreamEvent{ID: request.RequestID, Final: true}})
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}

// attachCredentials resolves the configured secret (when any) and attaches it
// per AuthScheme, then merges the static Headers. The resolved material is
// copied before use and cleared afterwards (mirroring the model connector);
// secrets are never included in errors or logs.
func (c *Connector) attachCredentials(ctx context.Context, request *http.Request) error {
	for name, value := range c.config.Headers {
		request.Header.Set(name, value)
	}
	if c.config.SecretRef == "" {
		return nil
	}
	secret, err := c.secrets.Resolve(ctx, c.config.SecretRef)
	if err != nil {
		return errors.New("resolve agent endpoint credential")
	}
	secretCopy := append([]byte(nil), secret...)
	defer clear(secretCopy)
	switch c.config.AuthScheme {
	case "bearer":
		request.Header.Set("Authorization", "Bearer "+string(secretCopy))
	case "x-api-key":
		request.Header.Set("x-api-key", string(secretCopy))
	case "":
		// A secret with no scheme is a configuration error surfaced by the
		// validator; fail closed here rather than silently dropping it.
		return errors.New("agent endpoint auth scheme is required")
	default:
		return errors.New("unsupported agent endpoint auth scheme")
	}
	return nil
}

// chatTurn reduces a canonical chat payload to the single A2A message the
// gateway relays: the last non-empty text turn, with its role mapped to the
// A2A vocabulary (assistant -> agent).
func chatTurn(chat *interaction.ChatPayload) (role, text string) {
	role = "user"
	for _, message := range chat.Messages {
		if message.Content == "" {
			continue
		}
		switch message.Role {
		case "assistant", "system", "developer":
			role = "agent"
		default:
			role = "user"
		}
		text = message.Content
	}
	return role, text
}

func (c *Connector) Health(context.Context, contracts.TargetRef) contracts.HealthStatus {
	return contracts.HealthStatus{Healthy: true, CheckedAt: c.now()}
}

func (c *Connector) Capabilities(context.Context, contracts.TargetRef) contracts.CapabilitySet {
	// a2a.streaming is only claimed because Stream genuinely consumes the SSE
	// streaming profile above; it is not a compliance claim about A2A's full
	// notification lifecycle.
	return contracts.CapabilitySet{"a2a": true, "message/send": true, "a2a.streaming": true}
}

func (c *Connector) NormalizeError(err error) *contracts.UpstreamError {
	var upstream *contracts.UpstreamError
	if errors.As(err, &upstream) {
		return upstream
	}
	// message/send is not idempotent: never auto-retry a send after an
	// ambiguous transport failure.
	return &contracts.UpstreamError{Code: "A2A_ERROR", Retryable: false, Message: "A2A invocation failed"}
}

var _ contracts.InteractionInvoker = (*Connector)(nil)
