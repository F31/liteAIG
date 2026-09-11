// Package server exposes the Lite data plane as OpenAI/Anthropic-compatible
// HTTP endpoints. It owns wire framing (including SSE) and delegates all
// governance decisions to the Core implementation supplied by the runtime.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/access/protocol/a2a"
	"github.com/F31/liteAIG/internal/access/protocol/anthropic"
	"github.com/F31/liteAIG/internal/access/protocol/mcp"
	"github.com/F31/liteAIG/internal/access/protocol/openai"
	"github.com/F31/liteAIG/internal/federation"
	guardraildomain "github.com/F31/liteAIG/internal/guardrail"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	kernelerrors "github.com/F31/liteAIG/internal/kernel/errors"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/platform/egress"
	"github.com/F31/liteAIG/internal/platform/webkit"
	routing "github.com/F31/liteAIG/internal/routing/model"
)

// AdmitInput carries everything the Core needs to authenticate and scope an
// inbound data-plane request.
type AdmitInput struct {
	Token     string
	RemoteIP  string
	Protocol  string
	BodyBytes int64
	Request   *interaction.UnifiedRequest
	// Federated carries inbound transport facts stamped by a trusted proxy for
	// the A2A surface. It is only set when the request has no usable bearer
	// token, and it is never populated from the JSON body. json:"-" keeps it
	// out of any accidental serialization of the admission input.
	Federated *FederatedTransport `json:"-"`
}

// FederatedTransport are transport-layer credentials of an inbound call as
// supplied by a trusted edge proxy (mTLS termination / OIDC token exchange /
// registry attestation). They are not client-assertable facts.
type FederatedTransport struct {
	AuthMethod string
	Subject    string
	Issuer     string
}

// Core is the governance surface the HTTP server depends on. The runtime wires
// authentication, admission, the governed pipeline, and model visibility.
type Core interface {
	// Admit authenticates the bearer token and returns a ready request context.
	Admit(ctx context.Context, input AdmitInput) (*kernel.RequestContext, error)
	// Run executes the governed pipeline for an admitted request, streaming to
	// writer when the request is streaming.
	Run(ctx context.Context, req *kernel.RequestContext, writer contracts.StreamWriter) error
	// Models returns the OpenAI-shaped model list visible to the bearer token.
	Models(ctx context.Context, token, remoteIP string) (*openai.ModelsResponse, error)
	// Tool invokes a governed tool/agent interaction and stores the canonical response.
	Tool(ctx context.Context, input AdmitInput) (*interaction.UnifiedResponse, error)
}

type Config struct {
	MaxBodyBytes int64
	// LocalAgentCard, when non-nil, is published at GET
	// /.well-known/agent-card.json with Cache-Control: public, max-age=300.
	// The card is operator-supplied static content for this process; a nil
	// value keeps the route registered but answering 404. There is no dynamic
	// per-tenant card editor in this package.
	LocalAgentCard *a2a.AgentCard
	// Middleware composes runtime-level concerns (e.g. the drain gate) into
	// the engine before the recovery middleware.
	Middleware []webkit.Middleware
	// CallbackClient is the egress-guarded HTTP client used for best-effort
	// A2A push callbacks. A nil client lazily builds an egress.LitePolicy
	// client on first use.
	CallbackClient *http.Client
	// A2APushOutbox, when set, makes A2A push callbacks durable: completed
	// callback payloads are stored and delivered by RunA2APushWorker with a
	// bounded retry budget. A nil store keeps the legacy one-shot callback path.
	A2APushOutbox federation.A2APushOutbox
	// BatchMappings persist OpenAI Batch IDs to the logical model that created
	// them so retrieve/cancel can route back to the same deployment.
	BatchMappings BatchMappingStore
	// A2APushSigningSecret signs durable and one-shot callback payloads with
	// HMAC-SHA256 headers. Empty means unsigned callbacks.
	A2APushSigningSecret []byte
	// A2APushSealBearer/OpenBearer protect callback bearer tokens while they are
	// queued durably. Lite wires these to the existing envelope cipher.
	A2APushSealBearer func([]byte) (string, error)
	A2APushOpenBearer func(string) ([]byte, error)
	// A2APushSealPayload/OpenPayload protect callback result bodies while they
	// are queued durably. Lite wires these to the existing envelope cipher.
	A2APushSealPayload func([]byte) ([]byte, error)
	A2APushOpenPayload func([]byte) ([]byte, error)
	// A2APushSealURL/OpenURL protect callback destinations while queued durably.
	A2APushSealURL func(string) (string, error)
	A2APushOpenURL func(string) (string, error)
}

type BatchMappingStore interface {
	SaveBatchMapping(context.Context, string, string) error
	ModelForBatch(context.Context, string) (string, error)
	SaveBatchFileMapping(context.Context, string, string, string) error
	ModelForBatchFile(context.Context, string) (string, error)
	SaveFileMapping(context.Context, string, string, string, string) error
	ModelForFile(context.Context, string) (string, error)
	DeleteFileMapping(context.Context, string) error
}

func (c Config) withDefaults() Config {
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = 1 << 20
	}
	return c
}

type Server struct {
	core Core
	cfg  Config
	logf func(format string, args ...any)

	httpOnce   sync.Once
	httpClient *http.Client
}

func New(core Core, cfg Config) *Server {
	return &Server{core: core, cfg: cfg.withDefaults(), logf: log.Printf}
}

// callbackClient returns the egress-guarded HTTP client used by the best-effort
// A2A push callback, preferring an operator-supplied client and otherwise
// building one lazily under the Lite egress policy (loopback allowed, no
// redirects, bounded bodies, protected dialing).
func (s *Server) callbackClient() *http.Client {
	if s.cfg.CallbackClient != nil {
		return s.cfg.CallbackClient
	}
	s.httpOnce.Do(func() {
		s.httpClient = egress.Client(egress.LitePolicy())
	})
	return s.httpClient
}

// Handler builds the data-plane engine: runtime middleware, kernel routing,
// and recovery (outermost), with protocol-specific error rendering kept at
// each protocol's writer.
func (s *Server) Handler() http.Handler {
	e := webkit.New()
	for _, mw := range s.cfg.Middleware {
		e.Use(mw)
	}
	e.Use(webkit.APICompatibility())
	e.Use(webkit.Recover())
	e.Handle("GET /.well-known/agent-card.json", s.agentCard)
	e.Handle("GET /v1/models", s.models)
	e.Handle("POST /v1/chat/completions", s.chat)
	e.Handle("POST /v1/responses", s.responses)
	e.Handle("POST /v1/embeddings", s.embeddings)
	e.Handle("POST /v1/rerank", s.rerank)
	e.Handle("POST /v1/audio/transcriptions", s.audioTranscriptions)
	e.Handle("POST /v1/batches", s.batchCreate)
	e.Handle("GET /v1/batches", s.batchList)
	e.Handle("GET /v1/batches/{id}", s.batchRetrieve)
	e.Handle("POST /v1/batches/{id}/cancel", s.batchCancel)
	e.Handle("POST /v1/files", s.fileUpload)
	e.Handle("GET /v1/files", s.fileList)
	e.Handle("GET /v1/files/{id}", s.fileRetrieve)
	e.Handle("DELETE /v1/files/{id}", s.fileDelete)
	e.Handle("GET /v1/files/{id}/content", s.fileContent)
	e.Handle("POST /v1/messages", s.messages)
	e.Handle("POST /mcp", s.mcp)
	e.Handle("POST /a2a", s.a2a)
	return e
}

// agentCard serves the local A2A Agent Card publication point. When no card
// is configured the route still answers (404 JSON) so discovery clients get a
// deterministic, well-formed negative response instead of a routing miss. The
// route never consults the Core: the card is static operator configuration.
func (s *Server) agentCard(c *webkit.Context) error {
	if s.cfg.LocalAgentCard == nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": map[string]any{"code": "NOT_FOUND"}})
	}
	c.Header().Set("Cache-Control", "public, max-age=300")
	return c.JSON(http.StatusOK, s.cfg.LocalAgentCard)
}

func (s *Server) models(c *webkit.Context) error {
	r := c.Request()
	list, err := s.core.Models(r.Context(), bearerToken(r), remoteIP(r))
	if err != nil {
		writeOpenAIError(c.Response(), err, "models")
		return nil
	}
	return c.JSON(http.StatusOK, list)
}

func (s *Server) chat(c *webkit.Context) error {
	r := c.Request()
	body, err := io.ReadAll(http.MaxBytesReader(c.Response(), r.Body, s.cfg.MaxBodyBytes))
	if err != nil {
		writeOpenAIError(c.Response(), err, "chat")
		return nil
	}
	request, err := openai.DecodeChatBytes(body)
	if err != nil {
		writeOpenAIError(c.Response(), &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "invalid chat request"}, "chat")
		return nil
	}
	s.invoke(c.Response(), r, request, "openai", openaiProtocol{}, false, int64(len(body)))
	return nil
}

// responses serves the minimal stateless OpenAI /v1/responses surface. The
// request decodes into Kind=responses with a projected Chat payload so the
// standard seven-stage pipeline runs unchanged; responses are excluded from
// the default cache kinds by kind. Both blocking and streaming are supported;
// streaming emits the Responses SSE event sequence (response.created …
// response.output_text.delta … response.completed).
func (s *Server) responses(c *webkit.Context) error {
	r := c.Request()
	body, err := io.ReadAll(http.MaxBytesReader(c.Response(), r.Body, s.cfg.MaxBodyBytes))
	if err != nil {
		writeOpenAIError(c.Response(), err, "responses")
		return nil
	}
	request, err := openai.DecodeResponsesBytes(body)
	if err != nil {
		writeOpenAIError(c.Response(), &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "invalid responses request"}, "responses")
		return nil
	}
	s.invoke(c.Response(), r, request, "responses", responsesProtocol{}, false, int64(len(body)))
	return nil
}

func (s *Server) embeddings(c *webkit.Context) error {
	r := c.Request()
	body, err := io.ReadAll(http.MaxBytesReader(c.Response(), r.Body, s.cfg.MaxBodyBytes))
	if err != nil {
		writeOpenAIError(c.Response(), err, "embeddings")
		return nil
	}
	request, err := openai.DecodeEmbeddingBytes(body)
	if err != nil {
		writeOpenAIError(c.Response(), &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "invalid embedding request"}, "embeddings")
		return nil
	}
	s.invoke(c.Response(), r, request, "openai", openaiProtocol{}, false, int64(len(body)))
	return nil
}

func (s *Server) rerank(c *webkit.Context) error {
	r := c.Request()
	body, err := io.ReadAll(http.MaxBytesReader(c.Response(), r.Body, s.cfg.MaxBodyBytes))
	if err != nil {
		writeOpenAIError(c.Response(), err, "rerank")
		return nil
	}
	request, err := openai.DecodeRerankBytes(body)
	if err != nil {
		writeOpenAIError(c.Response(), &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "invalid rerank request"}, "rerank")
		return nil
	}
	s.invoke(c.Response(), r, request, "openai", openaiProtocol{}, false, int64(len(body)))
	return nil
}

func (s *Server) audioTranscriptions(c *webkit.Context) error {
	r := c.Request()
	request, err := openai.DecodeAudioTranscription(r.Header.Get("Content-Type"), http.MaxBytesReader(c.Response(), r.Body, s.cfg.MaxBodyBytes))
	if err != nil {
		writeOpenAIError(c.Response(), &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "invalid audio transcription request"}, "audio")
		return nil
	}
	s.invoke(c.Response(), r, request, "openai", openaiProtocol{}, false, r.ContentLength)
	return nil
}

func (s *Server) batchCreate(c *webkit.Context) error {
	r := c.Request()
	body, err := io.ReadAll(http.MaxBytesReader(c.Response(), r.Body, s.cfg.MaxBodyBytes))
	if err != nil {
		writeOpenAIError(c.Response(), err, "batch")
		return nil
	}
	request, err := openai.DecodeBatchCreateBytes(body)
	if err != nil {
		writeOpenAIError(c.Response(), &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "invalid batch create request"}, "batch")
		return nil
	}
	admitted := s.invokeBatch(c.Response(), r, request, int64(len(body)))
	if admitted != nil {
		s.saveBatchMappings(r.Context(), admitted, request.Model)
	}
	return nil
}

func (s *Server) batchRetrieve(c *webkit.Context) error {
	if s.cfg.BatchMappings == nil {
		writeOpenAIError(c.Response(), &kernelerrors.Error{Code: "NOT_IMPLEMENTED", Message: "batch mapping store is not configured"}, "batch")
		return nil
	}
	id := c.Param("id")
	model, err := s.cfg.BatchMappings.ModelForBatch(c.Request().Context(), id)
	if err != nil {
		writeOpenAIError(c.Response(), &kernelerrors.Error{Code: "NOT_FOUND", Message: "batch mapping not found"}, "batch")
		return nil
	}
	admitted := s.invokeBatch(c.Response(), c.Request(), openai.NewBatchLifecycleRequest("retrieve", model, id, 0, ""), 0)
	if admitted != nil {
		s.saveBatchMappings(c.Request().Context(), admitted, model)
	}
	return nil
}

func (s *Server) fileUpload(c *webkit.Context) error {
	r := c.Request()
	request, err := openai.DecodeFileUpload(r.Header.Get("Content-Type"), http.MaxBytesReader(c.Response(), r.Body, s.cfg.MaxBodyBytes))
	if err != nil {
		writeOpenAIError(c.Response(), &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "invalid file upload request"}, "files")
		return nil
	}
	admitted := s.invokeBatch(c.Response(), r, request, r.ContentLength)
	if admitted != nil && admitted.Response != nil && admitted.Response.File != nil && admitted.Response.File.ID != "" && s.cfg.BatchMappings != nil {
		if err := s.cfg.BatchMappings.SaveFileMapping(r.Context(), admitted.Response.File.ID, request.Model, "upload", ""); err != nil {
			s.logf("file mapping save failed: %v", err)
		}
	}
	return nil
}

func (s *Server) fileList(c *webkit.Context) error {
	query := c.Request().URL.Query()
	model := query.Get("model")
	if model == "" {
		writeOpenAIError(c.Response(), &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "file list requires model query parameter"}, "files")
		return nil
	}
	s.invokeBatch(c.Response(), c.Request(), openai.NewFileLifecycleRequest("list", model, "", parsePositiveInt(query.Get("limit")), query.Get("after")), 0)
	return nil
}

func (s *Server) fileRetrieve(c *webkit.Context) error {
	model, ok := s.modelForFile(c, c.Param("id"))
	if !ok {
		return nil
	}
	s.invokeBatch(c.Response(), c.Request(), openai.NewFileLifecycleRequest("retrieve", model, c.Param("id"), 0, ""), 0)
	return nil
}

func (s *Server) fileDelete(c *webkit.Context) error {
	id := c.Param("id")
	model, ok := s.modelForFile(c, c.Param("id"))
	if !ok {
		return nil
	}
	admitted := s.invokeBatch(c.Response(), c.Request(), openai.NewFileLifecycleRequest("delete", model, id, 0, ""), 0)
	if admitted != nil && admitted.Response != nil && admitted.Response.File != nil && admitted.Response.File.Deleted {
		if err := s.cfg.BatchMappings.DeleteFileMapping(c.Request().Context(), id); err != nil {
			s.logf("file mapping delete failed: %v", err)
		}
	}
	return nil
}

func (s *Server) fileContent(c *webkit.Context) error {
	id := c.Param("id")
	model, ok := s.modelForFile(c, id)
	if !ok {
		return nil
	}
	request := openai.NewFileContentRequest(model, id)
	admitted := s.invokeBatch(c.Response(), c.Request(), request, 0)
	if admitted == nil {
		return nil
	}
	if admitted.Response == nil {
		writeOpenAIError(c.Response(), &kernelerrors.Error{Code: "UPSTREAM_ERROR", Message: "empty file response"}, "files")
		return nil
	}
	c.Response().Header().Set("Content-Type", "application/octet-stream")
	_, _ = c.Response().Write(admitted.Response.RawContent)
	return nil
}

func (s *Server) modelForFile(c *webkit.Context, id string) (string, bool) {
	if s.cfg.BatchMappings == nil {
		writeOpenAIError(c.Response(), &kernelerrors.Error{Code: "NOT_IMPLEMENTED", Message: "file mapping store is not configured"}, "files")
		return "", false
	}
	model, err := s.cfg.BatchMappings.ModelForFile(c.Request().Context(), id)
	if err != nil {
		writeOpenAIError(c.Response(), &kernelerrors.Error{Code: "NOT_FOUND", Message: "file mapping not found"}, "files")
		return "", false
	}
	return model, true
}

func (s *Server) saveBatchMappings(ctx context.Context, admitted *kernel.RequestContext, model string) {
	if s.cfg.BatchMappings == nil || admitted == nil || admitted.Response == nil || admitted.Response.Batch == nil || admitted.Response.Batch.ID == "" {
		return
	}
	batch := admitted.Response.Batch
	if err := s.cfg.BatchMappings.SaveBatchMapping(ctx, batch.ID, model); err != nil {
		s.logf("batch mapping save failed: %v", err)
	}
	for _, fileID := range []string{batch.OutputFileID, batch.ErrorFileID} {
		if err := s.cfg.BatchMappings.SaveBatchFileMapping(ctx, batch.ID, fileID, model); err != nil {
			s.logf("batch file mapping save failed: %v", err)
		}
	}
}

func (s *Server) batchCancel(c *webkit.Context) error {
	if s.cfg.BatchMappings == nil {
		writeOpenAIError(c.Response(), &kernelerrors.Error{Code: "NOT_IMPLEMENTED", Message: "batch mapping store is not configured"}, "batch")
		return nil
	}
	id := c.Param("id")
	model, err := s.cfg.BatchMappings.ModelForBatch(c.Request().Context(), id)
	if err != nil {
		writeOpenAIError(c.Response(), &kernelerrors.Error{Code: "NOT_FOUND", Message: "batch mapping not found"}, "batch")
		return nil
	}
	s.invokeBatch(c.Response(), c.Request(), openai.NewBatchLifecycleRequest("cancel", model, id, 0, ""), 0)
	return nil
}

func (s *Server) batchList(c *webkit.Context) error {
	query := c.Request().URL.Query()
	model := query.Get("model")
	if model == "" {
		writeOpenAIError(c.Response(), &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "batch list requires model query parameter"}, "batch")
		return nil
	}
	limit := parsePositiveInt(query.Get("limit"))
	s.invokeBatch(c.Response(), c.Request(), openai.NewBatchLifecycleRequest("list", model, "", limit, query.Get("after")), 0)
	return nil
}

func (s *Server) invokeBatch(w http.ResponseWriter, r *http.Request, request *interaction.UnifiedRequest, bodyBytes int64) *kernel.RequestContext {
	ctx := r.Context()
	admitted, err := s.core.Admit(ctx, AdmitInput{Token: bearerToken(r), RemoteIP: remoteIP(r), Protocol: "openai", BodyBytes: bodyBytes, Request: request})
	if err != nil {
		writeOpenAIError(w, err, "batch")
		return nil
	}
	if err := s.core.Run(ctx, admitted, nil); err != nil {
		writeOpenAIError(w, err, "batch")
		return nil
	}
	if request.Kind != interaction.RequestFile || request.File == nil || request.File.Operation != "content" {
		s.render(w, openaiProtocol{}, admitted, request.Model)
	}
	return admitted
}

func parsePositiveInt(value string) int {
	var out int
	_, _ = fmt.Sscanf(value, "%d", &out)
	if out < 0 {
		return 0
	}
	return out
}

func (s *Server) messages(c *webkit.Context) error {
	r := c.Request()
	body, err := io.ReadAll(http.MaxBytesReader(c.Response(), r.Body, s.cfg.MaxBodyBytes))
	if err != nil {
		writeAnthropicError(c.Response(), err, "messages")
		return nil
	}
	request, err := anthropic.DecodeMessagesBytes(body)
	if err != nil {
		writeAnthropicError(c.Response(), &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "invalid messages request"}, "messages")
		return nil
	}
	s.invoke(c.Response(), r, request, "anthropic", anthropicProtocol{}, true, int64(len(body)))
	return nil
}

func (s *Server) mcp(c *webkit.Context) error {
	r := c.Request()
	w := c.Response()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes))
	if err != nil {
		writeMCPError(w, nil, err)
		return nil
	}
	var envelope struct {
		ID any `json:"id"`
	}
	_ = json.Unmarshal(body, &envelope)
	request, err := mcp.Normalize(body)
	if err != nil {
		writeMCPError(w, envelope.ID, &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "invalid MCP request"})
		return nil
	}
	response, err := s.core.Tool(r.Context(), AdmitInput{Token: bearerToken(r), RemoteIP: remoteIP(r), Protocol: "mcp", BodyBytes: int64(len(body)), Request: request})
	if err != nil {
		writeMCPError(w, envelope.ID, err)
		return nil
	}
	content := ""
	if response != nil && response.ToolResult != nil {
		content = response.ToolResult.Content
	}
	if request.Tool != nil && request.Tool.Name == "server/discover" {
		var result map[string]any
		if json.Unmarshal([]byte(content), &result) == nil {
			writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": envelope.ID, "result": result})
			return nil
		}
	}
	if strings.HasPrefix(mcp.Method(request), "tasks/") {
		var result any
		if json.Unmarshal([]byte(content), &result) == nil {
			writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": envelope.ID, "result": result})
			return nil
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": envelope.ID, "result": map[string]any{"content": content}})
	return nil
}

func (s *Server) a2a(c *webkit.Context) error {
	r := c.Request()
	w := c.Response()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes))
	if err != nil {
		writeA2AError(w, nil, err)
		return nil
	}
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	_ = json.Unmarshal(body, &envelope)

	// Strict A2A JSON-RPC surface: a request carrying a method must speak
	// JSON-RPC 2.0 and dispatch to a supported method. Anything else is a
	// JSON-RPC error instead of an accidental agent invocation.
	federated := federatedTransport(r)
	var push *PushNotificationConfig
	if len(envelope.Params) > 0 {
		var params struct {
			Push *PushNotificationConfig `json:"pushNotificationConfig"`
		}
		_ = json.Unmarshal(envelope.Params, &params)
		push = params.Push
	}
	if envelope.Method != "" {
		if envelope.JSONRPC != "2.0" {
			writeA2ARPCError(w, envelope.ID, http.StatusBadRequest, rpcInvalidRequest, "invalid request")
			return nil
		}
		// The 0.3-style "message/send" and the official SDK 1.0 JSON-RPC
		// method names dispatch to the same relay. The method selects the
		// response wire format and, for SendStreamingMessage, forces the SDK's
		// JSON-RPC-over-SSE stream instead of the LiteAIG SSE profile.
		v1 := envelope.Method == a2aMethodSendMessageV1 || envelope.Method == a2aMethodSendStreamingMessageV1
		streamV1 := envelope.Method == a2aMethodSendStreamingMessageV1
		if !v1 && envelope.Method != a2aMethodMessageSend {
			writeA2ARPCError(w, envelope.ID, http.StatusNotFound, rpcMethodNotFound, "method not found")
			return nil
		}
		var params struct {
			Message json.RawMessage `json:"message"`
		}
		if json.Unmarshal(envelope.Params, &params) != nil || len(params.Message) == 0 {
			writeA2ARPCError(w, envelope.ID, http.StatusBadRequest, rpcInvalidParams, "invalid params: message is required")
			return nil
		}
		request, err := a2a.NormalizeTaskMessage(params.Message)
		if err != nil {
			writeA2ARPCError(w, envelope.ID, http.StatusBadRequest, rpcInvalidParams, "invalid params: "+err.Error())
			return nil
		}
		s.serveA2ARelay(w, r, envelope.ID, request, int64(len(body)), federated, push, v1, streamV1)
		return nil
	}

	// Legacy convenience: a bare A2A message (or a method-less envelope whose
	// params carry a message) is treated as message/send.
	payload := body
	if len(envelope.Params) > 0 {
		var params struct {
			Message json.RawMessage `json:"message"`
		}
		if json.Unmarshal(envelope.Params, &params) == nil && len(params.Message) > 0 {
			payload = params.Message
		}
	}
	request, err := a2a.NormalizeTaskMessage(payload)
	if err != nil {
		writeA2AError(w, envelope.ID, &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "invalid A2A request"})
		return nil
	}
	s.serveA2ARelay(w, r, envelope.ID, request, int64(len(body)), federated, push, false, false)
	return nil
}

// JSON-RPC standard error codes for the A2A surface.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

// A2A JSON-RPC method names: the 0.3 profile ("message/send") and the official
// SDK 1.0 method names dispatch to the relay; the method selects the response
// wire format.
const (
	a2aMethodMessageSend            = "message/send"
	a2aMethodSendMessageV1          = "SendMessage"
	a2aMethodSendStreamingMessageV1 = "SendStreamingMessage"
)

func writeA2ARPCError(w http.ResponseWriter, id any, status, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

// invoke admits the request, runs the pipeline, and renders the response in the
// caller's protocol. Streaming requests get an SSE body; otherwise a JSON body.
func (s *Server) invoke(w http.ResponseWriter, r *http.Request, request *interaction.UnifiedRequest, protocol string, enc encoder, anthropicProtocol bool, bodyBytes int64) {
	ctx := r.Context()
	admitted, err := s.core.Admit(ctx, AdmitInput{
		Token:     bearerToken(r),
		RemoteIP:  remoteIP(r),
		Protocol:  protocol,
		BodyBytes: bodyBytes,
		Request:   request,
	})
	if err != nil {
		if anthropicProtocol {
			writeAnthropicError(w, err, protocol)
		} else {
			writeOpenAIError(w, err, protocol)
		}
		return
	}
	model := request.Model
	if request.Stream {
		flusher, ok := w.(http.Flusher)
		if !ok {
			s.writeError(w, anthropicProtocol, &kernelerrors.Error{Code: "STREAMING_UNSUPPORTED", Message: "streaming is not supported by this server"}, protocol)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		writer := newSSEWriter(w, flusher, enc.streamEncoder(model))
		if err := s.core.Run(ctx, admitted, writer); err != nil {
			// The stream has already started; emit a terminal error frame.
			_ = writer.fail(err)
		}
		return
	}
	if err := s.core.Run(ctx, admitted, nil); err != nil {
		s.writeError(w, anthropicProtocol, err, protocol)
		return
	}
	s.render(w, enc, admitted, model)
}

func (s *Server) render(w http.ResponseWriter, enc encoder, req *kernel.RequestContext, model string) {
	var payload []byte
	var err error
	switch enc.name() {
	case "openai":
		if req.Request.Kind == interaction.RequestEmbedding {
			payload, err = openai.EncodeEmbeddingResponse(req.Response, model)
		} else if req.Request.Kind == interaction.RequestRerank {
			payload, err = openai.EncodeRerankResponse(req.Response, model)
		} else if req.Request.Kind == interaction.RequestAudio {
			payload, err = openai.EncodeAudioTranscriptionResponse(req.Response)
		} else if req.Request.Kind == interaction.RequestBatch {
			payload, err = openai.EncodeBatchResponse(req.Response)
		} else if req.Request.Kind == interaction.RequestFile {
			payload, err = openai.EncodeFileResponse(req.Response)
		} else {
			payload, err = openai.EncodeChatResponse(req.Response, model)
		}
	case "responses":
		payload, err = openai.EncodeResponsesResponse(req.Response, model)
	case "anthropic":
		payload, err = anthropic.EncodeResponse(req.Response, model)
	default:
		err = errors.New("unknown protocol")
	}
	if err != nil {
		s.writeError(w, enc.name() == "anthropic", err, enc.name())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}

func (s *Server) writeError(w http.ResponseWriter, anthropicProtocol bool, err error, protocol string) {
	if anthropicProtocol {
		writeAnthropicError(w, err, protocol)
	} else {
		writeOpenAIError(w, err, protocol)
	}
}

// encoder abstracts the per-protocol response/stream rendering.
type encoder interface {
	name() string
	streamEncoder(model string) streamFrameEncoder
}

// streamFrameEncoder turns one normalized stream event into SSE frames.
type streamFrameEncoder interface {
	encode(event interaction.StreamEvent) ([]sseFrame, error)
	final() []sseFrame
}

type sseFrame struct {
	name string
	data []byte
}

type openaiProtocol struct{}

func (openaiProtocol) name() string { return "openai" }
func (openaiProtocol) streamEncoder(model string) streamFrameEncoder {
	return openaiStreamEncoder{model: model}
}

type openaiStreamEncoder struct{ model string }

func (e openaiStreamEncoder) encode(event interaction.StreamEvent) ([]sseFrame, error) {
	if event.Final && event.Delta == "" && event.StopReason == "" && event.Usage == nil {
		return nil, nil
	}
	data, err := openai.EncodeStreamChunk(event, e.model)
	if err != nil {
		return nil, err
	}
	return []sseFrame{{data: data}}, nil
}
func (e openaiStreamEncoder) final() []sseFrame { return []sseFrame{{data: openai.StreamDone()}} }

type anthropicProtocol struct{}

func (anthropicProtocol) name() string { return "anthropic" }
func (p anthropicProtocol) streamEncoder(model string) streamFrameEncoder {
	return &anthropicStreamEncoder{enc: anthropic.NewStreamEncoder(model)}
}

// responsesProtocol renders the stateless /v1/responses wire shape, both
// blocking JSON and the Responses SSE event stream.
type responsesProtocol struct{}

func (responsesProtocol) name() string { return "responses" }
func (responsesProtocol) streamEncoder(model string) streamFrameEncoder {
	return &responsesStreamEncoder{model: model}
}

// responsesStreamEncoder emits the OpenAI Responses SSE event sequence that
// the official SDK consumes: response.created (carrying an empty output
// skeleton), response.in_progress, then per text chunk a
// response.output_item.added / response.content_part.added preamble and
// response.output_text.delta events, and on completion
// response.output_text.done / response.content_part.done /
// response.output_item.done / response.completed (with usage). Tool-call
// deltas are not part of the minimal stream surface and are ignored.
type responsesStreamEncoder struct {
	model    string
	started  bool
	text     strings.Builder
	response string
	item     string
	created  int64
	usage    *interaction.UnifiedUsage
}

const (
	responsesResponseID = "resp_liteaig"
	responsesItemID     = "msg_liteaig"
)

func (e *responsesStreamEncoder) encode(event interaction.StreamEvent) ([]sseFrame, error) {
	if e.response == "" {
		e.response = responsesResponseID
	}
	if e.item == "" {
		e.item = responsesItemID
	}
	if e.created == 0 {
		e.created = time.Now().Unix()
	}
	if event.Usage != nil {
		usage := *event.Usage
		e.usage = &usage
	}
	if !e.started {
		e.started = true
		frames, err := e.preamble()
		if err != nil {
			return nil, err
		}
		if event.Delta == "" {
			return frames, nil
		}
		e.text.WriteString(event.Delta)
		delta, err := sseJSON(map[string]any{
			"type": "response.output_text.delta", "item_id": e.item,
			"output_index": 0, "content_index": 0, "delta": event.Delta,
		})
		if err != nil {
			return nil, err
		}
		return append(frames, delta), nil
	}
	if event.Delta == "" {
		return nil, nil
	}
	e.text.WriteString(event.Delta)
	delta, err := sseJSON(map[string]any{
		"type": "response.output_text.delta", "item_id": e.item,
		"output_index": 0, "content_index": 0, "delta": event.Delta,
	})
	if err != nil {
		return nil, err
	}
	return []sseFrame{delta}, nil
}

func (e *responsesStreamEncoder) final() []sseFrame {
	fullText := e.text.String()
	content := []any{map[string]any{"type": "output_text", "text": fullText, "annotations": []any{}}}
	var frames []sseFrame
	textDone, _ := sseJSON(map[string]any{
		"type": "response.output_text.done", "item_id": e.item,
		"output_index": 0, "content_index": 0, "text": fullText, "logprobs": []any{},
	})
	frames = append(frames, textDone)
	frames = append(frames, mustFrame(map[string]any{
		"type": "response.content_part.done", "item_id": e.item,
		"output_index": 0, "content_index": 0,
		"part": map[string]any{"type": "output_text", "text": fullText, "annotations": []any{}},
	}))
	frames = append(frames, mustFrame(map[string]any{
		"type": "response.output_item.done", "output_index": 0,
		"item": map[string]any{"id": e.item, "type": "message", "status": "completed", "role": "assistant", "content": content},
	}))
	response := map[string]any{
		"id": e.response, "object": "response", "created_at": e.created,
		"status": "completed", "model": e.model, "output": []any{
			map[string]any{"id": e.item, "type": "message", "status": "completed", "role": "assistant", "content": content},
		},
	}
	if e.usage != nil {
		response["usage"] = map[string]any{
			"input_tokens": e.usage.InputTokens, "output_tokens": e.usage.OutputTokens,
			"total_tokens": e.usage.TotalTokens(),
		}
	} else {
		response["usage"] = nil
	}
	return append(frames, mustFrame(map[string]any{"type": "response.completed", "response": response}))
}

func (e *responsesStreamEncoder) preamble() ([]sseFrame, error) {
	skeleton := map[string]any{
		"id": e.response, "object": "response", "created_at": e.created,
		"status": "in_progress", "model": e.model, "output": []any{},
	}
	created, err := sseJSON(map[string]any{"type": "response.created", "response": skeleton})
	if err != nil {
		return nil, err
	}
	inProgress, err := sseJSON(map[string]any{"type": "response.in_progress", "response": skeleton})
	if err != nil {
		return nil, err
	}
	itemAdded, err := sseJSON(map[string]any{
		"type": "response.output_item.added", "output_index": 0,
		"item": map[string]any{"id": e.item, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}},
	})
	if err != nil {
		return nil, err
	}
	partAdded, err := sseJSON(map[string]any{
		"type": "response.content_part.added", "item_id": e.item,
		"output_index": 0, "content_index": 0,
		"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
	})
	if err != nil {
		return nil, err
	}
	return []sseFrame{created, inProgress, itemAdded, partAdded}, nil
}

func sseJSON(payload any) (sseFrame, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return sseFrame{}, err
	}
	return sseFrame{data: data}, nil
}

func mustFrame(payload any) sseFrame {
	frames, err := sseJSON(payload)
	if err != nil {
		return sseFrame{}
	}
	return frames
}

type anthropicStreamEncoder struct{ enc *anthropic.StreamEncoder }

func (e *anthropicStreamEncoder) encode(event interaction.StreamEvent) ([]sseFrame, error) {
	frames, err := e.enc.Write(event)
	if err != nil {
		return nil, err
	}
	out := make([]sseFrame, len(frames))
	for i, f := range frames {
		out[i] = sseFrame{name: f.Name, data: f.Data}
	}
	return out, nil
}
func (e *anthropicStreamEncoder) final() []sseFrame { return nil }

// sseWriter implements contracts.StreamWriter over an HTTP SSE response.
type sseWriter struct {
	w         http.ResponseWriter
	flusher   http.Flusher
	enc       streamFrameEncoder
	model     string
	finalSent bool
	failed    bool
}

func newSSEWriter(w http.ResponseWriter, flusher http.Flusher, enc streamFrameEncoder) *sseWriter {
	return &sseWriter{w: w, flusher: flusher, enc: enc}
}

func (s *sseWriter) WriteChunk(_ context.Context, chunk contracts.StreamChunk) error {
	if s.failed {
		return nil
	}
	frames, err := s.enc.encode(chunk.Event)
	if err != nil {
		s.failed = true
		return err
	}
	if err := s.writeFrames(frames); err != nil {
		s.failed = true
		return err
	}
	if chunk.Event.Final && !s.finalSent {
		s.finalSent = true
		if final := s.enc.final(); len(final) > 0 {
			if err := s.writeFrames(final); err != nil {
				s.failed = true
				return err
			}
		}
	}
	return nil
}

func (s *sseWriter) writeFrames(frames []sseFrame) error {
	for _, f := range frames {
		var b strings.Builder
		if f.name != "" {
			b.WriteString("event: ")
			b.WriteString(f.name)
			b.WriteByte('\n')
		}
		b.WriteString("data: ")
		b.Write(f.data)
		b.WriteString("\n\n")
		if _, err := s.w.Write([]byte(b.String())); err != nil {
			return err
		}
	}
	s.flusher.Flush()
	return nil
}

// fail emits a terminal error frame once the stream has started.
func (s *sseWriter) fail(err error) error {
	if s.failed {
		return nil
	}
	s.failed = true
	status, message := errorStatus(err)
	payload, _ := json.Marshal(map[string]string{"error": message, "code": errorCode(err)})
	var b strings.Builder
	b.WriteString("event: error\n")
	b.WriteString("data: ")
	b.Write(payload)
	b.WriteString("\n\n")
	_, _ = s.w.Write([]byte(b.String()))
	s.flusher.Flush()
	_ = status
	return nil
}

func bearerToken(r *http.Request) string {
	value := r.Header.Get("Authorization")
	if value == "" {
		return strings.TrimSpace(r.Header.Get("X-Api-Key"))
	}
	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return strings.TrimSpace(value[len("bearer "):])
	}
	return value
}

// federatedTransport extracts inbound transport facts set by a trusted proxy.
//
// Trusted-proxy headers (documented contract with the edge):
//   - X-A2A-Auth-Method: mtls_spki | oidc | jws | registry_attestation (or the
//     equivalent federated_* spellings)
//   - X-A2A-Subject: the verified anchor subject
//   - X-A2A-Issuer: optional OIDC issuer / registry
//
// These headers are ONLY honored when the request carries no usable bearer
// token, and facts without both an auth method and a subject are ignored: a
// bearer-authenticated caller must never be able to add a federated identity
// alongside its key. They must be stripped (or overwritten) by the proxy for
// any client-supplied copy to have no effect.
func federatedTransport(r *http.Request) *FederatedTransport {
	if bearerToken(r) != "" {
		return nil
	}
	method := strings.TrimSpace(r.Header.Get("X-A2A-Auth-Method"))
	subject := strings.TrimSpace(r.Header.Get("X-A2A-Subject"))
	if method == "" || subject == "" {
		return nil
	}
	return &FederatedTransport{
		AuthMethod: method,
		Subject:    subject,
		Issuer:     strings.TrimSpace(r.Header.Get("X-A2A-Issuer")),
	}
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeOpenAIError(w http.ResponseWriter, err error, protocol string) {
	status, message := errorStatus(err)
	body := openai.NewErrorResponse(errorCode(err), message)
	w.Header().Set("Content-Type", "application/json")
	if isRetryable(err) {
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeAnthropicError(w http.ResponseWriter, err error, protocol string) {
	status, message := errorStatus(err)
	body := map[string]any{"type": "error", "error": map[string]string{"type": errorType(err), "message": message}}
	w.Header().Set("Content-Type", "application/json")
	if isRetryable(err) {
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeMCPError(w http.ResponseWriter, id any, err error) {
	status, message := errorStatus(err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": errorCode(err), "message": message},
	})
}

func writeA2AError(w http.ResponseWriter, id any, err error) {
	status, message := errorStatus(err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": errorCode(err), "message": message},
	})
}

func errorStatus(err error) (int, string) {
	var kernelErr *kernelerrors.Error
	if errors.As(err, &kernelErr) {
		return statusForCode(kernelErr.Code), kernelErr.Message
	}
	var blockErr *guardraildomain.BlockError
	if errors.As(err, &blockErr) {
		return http.StatusBadRequest, blockErr.Message
	}
	var upstream *contracts.UpstreamError
	if errors.As(err, &upstream) {
		if upstream.StatusCode == http.StatusTooManyRequests {
			return http.StatusTooManyRequests, upstream.Message
		}
		return http.StatusBadGateway, fmt.Sprintf("upstream error: %s", upstream.Message)
	}
	if errors.Is(err, routing.ErrNoEligibleDeployment) {
		return http.StatusServiceUnavailable, "no eligible deployment"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return http.StatusGatewayTimeout, "request timed out"
	}
	return http.StatusInternalServerError, "internal error"
}

func errorCode(err error) string {
	var kernelErr *kernelerrors.Error
	if errors.As(err, &kernelErr) {
		return kernelErr.Code
	}
	var blockErr *guardraildomain.BlockError
	if errors.As(err, &blockErr) {
		return "GUARDRAIL_BLOCKED"
	}
	var upstream *contracts.UpstreamError
	if errors.As(err, &upstream) {
		return upstream.Code
	}
	return "INTERNAL_ERROR"
}

func errorType(err error) string {
	var kernelErr *kernelerrors.Error
	if errors.As(err, &kernelErr) {
		return strings.ToLower(kernelErr.Code)
	}
	return "api_error"
}

func isRetryable(err error) bool {
	var kernelErr *kernelerrors.Error
	if errors.As(err, &kernelErr) {
		return kernelErr.Retryable
	}
	var upstream *contracts.UpstreamError
	if errors.As(err, &upstream) {
		return upstream.Retryable
	}
	return false
}

func statusForCode(code string) int {
	return kernelerrors.StatusForCode(code)
}
