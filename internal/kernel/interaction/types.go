// Package interaction defines protocol-neutral interaction and model request types.
package interaction

import (
	"encoding/json"
	"strings"
	"time"
)

// Kind identifies the governed target of an interaction.
type Kind string

const (
	KindModel    Kind = "model"
	KindTool     Kind = "tool"
	KindAgent    Kind = "agent"
	KindApproval Kind = "approval"
)

// PrincipalRef identifies a canonical caller without embedding identity metadata.
type PrincipalRef struct {
	Type string
	ID   string
}

// ResourceRef identifies a governed target resource.
type ResourceRef struct {
	Type string
	ID   string
}

// TrustBoundary distinguishes internal from external federated targets.
type TrustBoundary string

const (
	TrustBoundaryInternal          TrustBoundary = "internal"
	TrustBoundaryExternalFederated TrustBoundary = "external_federated"
)

// Direction is the flow of an interaction relative to this deployment.
type Direction string

const (
	DirectionInternal Direction = "internal"
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
)

// FederationContext carries cross-organization trust facts.
type FederationContext struct {
	RelationshipID  string
	ExternalAgentID string
	AssuranceLevel  string
	DataBoundary    string
	TrustAnchorID   string
}

// Context carries protocol-neutral scope and provenance through the pipeline.
type Context struct {
	Kind            Kind
	Protocol        string
	TenantID        string
	ProjectID       string
	SessionID       string
	TaskID          string
	RootTaskID      string
	ParentTaskID    string
	ParentRequestID string
	TrustBoundary   TrustBoundary
	Direction       Direction
	Federation      *FederationContext
	Caller          PrincipalRef
	Target          ResourceRef
}

// IsExternalFederated reports whether the interaction crosses a trust boundary.
func (c *Context) IsExternalFederated() bool {
	return c.TrustBoundary == TrustBoundaryExternalFederated
}

// RequestKind identifies a canonical model request shape.
type RequestKind string

const (
	RequestChat      RequestKind = "chat"
	RequestResponses RequestKind = "responses"
	RequestEmbedding RequestKind = "embedding"
	RequestRerank    RequestKind = "rerank"
	RequestAudio     RequestKind = "audio"
	RequestBatch     RequestKind = "batch"
	RequestFile      RequestKind = "file"
	RequestTool      RequestKind = "tool"
)

// UnifiedRequest contains fields shared by the Phase 0 protocol surfaces.
// Protocol payloads are added as canonical types by the model contract task.
type UnifiedRequest struct {
	Kind         RequestKind
	Model        string
	Stream       bool
	Metadata     map[string]string
	SessionID    string
	TaskID       string
	RootTaskID   string
	ParentTaskID string
	Chat         *ChatPayload
	Responses    *ResponsesPayload
	Embedding    *EmbeddingPayload
	Rerank       *RerankPayload
	Audio        *AudioPayload
	Batch        *BatchPayload
	File         *FilePayload
	Tool         *ToolPayload
	Parameters   map[string]json.RawMessage
}

// MarshalJSON keeps the serialized shape of text-only (non-responses) requests
// byte-identical to the pre-Stage-15 layout: Responses is omitted when nil, so
// cache-key normalization and the A2A outbound body do not change for the
// kinds that predate /v1/responses.
func (u UnifiedRequest) MarshalJSON() ([]byte, error) {
	// Tags reproduce the pre-Stage-15 JSON keys exactly (Go field names with no
	// lowercasing); only the new Responses pointer is omitted when nil.
	type requestJSON struct {
		Kind         RequestKind                `json:"Kind"`
		Model        string                     `json:"Model"`
		Stream       bool                       `json:"Stream"`
		Metadata     map[string]string          `json:"Metadata"`
		SessionID    string                     `json:"SessionID"`
		TaskID       string                     `json:"TaskID"`
		RootTaskID   string                     `json:"RootTaskID"`
		ParentTaskID string                     `json:"ParentTaskID"`
		Chat         *ChatPayload               `json:"Chat"`
		Responses    *ResponsesPayload          `json:"Responses,omitempty"`
		Embedding    *EmbeddingPayload          `json:"Embedding"`
		Rerank       *RerankPayload             `json:"Rerank,omitempty"`
		Audio        *AudioPayload              `json:"Audio,omitempty"`
		Batch        *BatchPayload              `json:"Batch,omitempty"`
		File         *FilePayload               `json:"File,omitempty"`
		Tool         *ToolPayload               `json:"Tool"`
		Parameters   map[string]json.RawMessage `json:"Parameters"`
	}
	return json.Marshal(requestJSON{
		Kind: u.Kind, Model: u.Model, Stream: u.Stream, Metadata: u.Metadata,
		SessionID: u.SessionID, TaskID: u.TaskID, RootTaskID: u.RootTaskID,
		ParentTaskID: u.ParentTaskID, Chat: u.Chat, Responses: u.Responses,
		Embedding: u.Embedding, Rerank: u.Rerank, Audio: u.Audio, Batch: u.Batch, File: u.File, Tool: u.Tool, Parameters: u.Parameters,
	})
}

func (u *UnifiedRequest) UnmarshalJSON(data []byte) error {
	type noMethod UnifiedRequest
	var decoded noMethod
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*u = UnifiedRequest(decoded)
	return nil
}

// Tool declares a client-provided function for model-side function calling.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall is a model-generated (or client-echoed) function call. Arguments is
// the JSON object as a string, per the OpenAI wire convention.
type ToolCall struct {
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ToolCallDelta is one incremental tool-call fragment in a stream. Index is
// the provider-side position (OpenAI tool-call index or Anthropic block
// index); a fragment that carries no ID/Name references its call by Index.
type ToolCallDelta struct {
	Index     int
	ID        string
	Type      string
	Name      string
	Arguments string
}

// ContentPartKind distinguishes the media kind of a content part.
type ContentPartKind string

const (
	// ContentText is a plain-text part. Text parts are projected onto
	// Message.Content and are not duplicated in Message.Parts.
	ContentText ContentPartKind = "text"
	// ContentImage is an image part carried as an inline data: URI and/or a
	// remote http(s) reference.
	ContentImage ContentPartKind = "image"
)

// ContentPart is one content element of a multimodal message. The canonical
// model keeps text in Message.Content (the text-only projection every earlier
// stage reads) and stores only the non-text parts here, preserving their
// relative order for wire re-encoding. Providers translate image parts to
// their own wire form (OpenAI image_url, Anthropic image blocks).
type ContentPart struct {
	Kind ContentPartKind

	// MediaType is the image media type (image/png, image/jpeg, ...).
	MediaType string
	// DataURL is the inline image as a data: URI
	// (data:<media-type>;base64,<payload>) when the client supplied one.
	DataURL string
	// URL is a remote http(s) image reference when the client supplied one.
	URL string
}

type Message struct {
	Role       string
	Content    string
	ToolCallID string     // set on role:"tool" messages (the call being answered)
	ToolCalls  []ToolCall // set on assistant messages that carry function calls
	// CacheControl marks a prompt-cacheable content block for providers that
	// offer explicit prompt caching (Anthropic's cache_control). It is
	// "ephemeral" (the only provider-supported value today) or empty. The
	// gateway passes it through so a client's cache markers survive the
	// decode→re-encode round trip for Anthropic; other adapters ignore it.
	CacheControl string
	// Parts carries ordered non-text (image) content parts when the message is
	// multimodal. Empty for text-only messages; see ContentPart.
	Parts []ContentPart
}

// MarshalJSON keeps text-only Message serialization byte-identical to the
// pre-Stage-15 layout by omitting Parts when empty.
func (m Message) MarshalJSON() ([]byte, error) {
	type messageJSON struct {
		Role         string        `json:"Role"`
		Content      string        `json:"Content"`
		ToolCallID   string        `json:"ToolCallID"`
		ToolCalls    []ToolCall    `json:"ToolCalls"`
		CacheControl string        `json:"CacheControl"`
		Parts        []ContentPart `json:"Parts,omitempty"`
	}
	return json.Marshal(messageJSON{
		Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID,
		ToolCalls: m.ToolCalls, CacheControl: m.CacheControl, Parts: m.Parts,
	})
}

func (m *Message) UnmarshalJSON(data []byte) error {
	type noMethod Message
	var decoded noMethod
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*m = Message(decoded)
	return nil
}

// HasImages reports whether the message carries any image content part.
func (m Message) HasImages() bool {
	for _, part := range m.Parts {
		if part.Kind == ContentImage {
			return true
		}
	}
	return false
}

// AddImage appends an image content part to the message.
func (m *Message) AddImage(part ContentPart) {
	part.Kind = ContentImage
	m.Parts = append(m.Parts, part)
}

// ResponsesPayload is the minimal, stateless OpenAI /v1/responses surface
// (spec §5.1 P0-Commercial). It carries the messages derived from the
// Responses `input` items plus top-level options; the gateway projects it onto
// a ChatPayload so the existing seven-stage pipeline runs unchanged.
type ResponsesPayload struct {
	Model           string
	Messages        []Message
	MaxOutputTokens *int
	Temperature     *float64
	TopP            *float64
	Tools           []Tool
	ToolChoice      json.RawMessage
}

type ChatPayload struct {
	Messages        []Message
	MaxOutputTokens *int
	Temperature     *float64
	TopP            *float64
	Stop            []string
	Tools           []Tool
	ToolChoice      json.RawMessage
	ResponseFormat  json.RawMessage
}
type EmbeddingPayload struct{ Inputs []string }
type RerankPayload struct {
	Query     string
	Documents []string
	TopN      *int
}
type AudioPayload struct {
	Filename       string
	MediaType      string
	Data           []byte
	Language       string
	Prompt         string
	ResponseFormat string
	Temperature    *float64
}
type BatchPayload struct {
	Operation        string
	ID               string
	InputFileID      string
	Endpoint         string
	CompletionWindow string
	Limit            int
	After            string
	Metadata         map[string]string
}

type FilePayload struct {
	Operation string
	ID        string
	Purpose   string
	Filename  string
	MediaType string
	Data      []byte
	Limit     int
	After     string
}

type Provenance struct {
	Source  string
	Trusted bool
}

type ToolPayload struct {
	Name       string
	Arguments  json.RawMessage
	Result     string
	Provenance Provenance
}

// UnifiedResponse is the protocol-neutral upstream result envelope.
type UnifiedResponse struct {
	ID         string
	Model      string
	StopReason string
	Usage      UnifiedUsage
	CreatedAt  time.Time
	Choices    []Choice
	Embeddings [][]float64
	Rerank     []RerankResult
	Audio      *AudioResult
	Batch      *BatchResult
	File       *FileResult
	RawContent []byte
	ToolResult *ToolResult
}

type AudioResult struct{ Text string }
type BatchResult struct {
	ID               string
	Object           string
	Endpoint         string
	InputFileID      string
	OutputFileID     string
	ErrorFileID      string
	Status           string
	CompletionWindow string
	Items            []BatchResult
	HasMore          bool
	FirstID          string
	LastID           string
	Raw              json.RawMessage
}

type FileResult struct {
	ID        string
	Object    string
	Bytes     int64
	CreatedAt int64
	Filename  string
	Purpose   string
	Deleted   bool
	Items     []FileResult
	HasMore   bool
	Raw       json.RawMessage
}

type RerankResult struct {
	Index          int
	RelevanceScore float64
	Document       string
}

type ToolResult struct {
	Name, Content string
	Provenance    Provenance
}

type Choice struct {
	Index   int
	Message Message
}
type StreamEvent struct {
	ID, Model string
	Index     int
	Delta     string
	// ToolCallDeltas carries incremental function-call fragments; empty for
	// plain text streams.
	ToolCallDeltas []ToolCallDelta
	StopReason     string
	Usage          *UnifiedUsage
	Final          bool
}

type ParameterMode string

const (
	ParametersStrict     ParameterMode = "strict"
	ParametersPermissive ParameterMode = "permissive"
)

type ParameterSupport struct {
	Supported            map[string]bool
	PassthroughAllowlist map[string]bool
}
type ParameterWarning struct{ Name, Code string }

// UsageSource enumerates how a UnifiedUsage fact was obtained.
type UsageSource string

const (
	UsageProvider UsageSource = "provider"
	UsageCountAPI UsageSource = "count_api"
	UsageLocalEst UsageSource = "local_estimate"
)

// UnifiedUsage is the normalized token fact used by accounting. The input and
// output token families follow the text/reasoning token口径; image and audio
// units are tracked separately where a provider reports them.
type UnifiedUsage struct {
	InputTokens       int64
	OutputTokens      int64
	CacheReadTokens   int64
	CacheWriteTokens  int64
	CachedInputTokens int64
	ReasoningTokens   int64
	AudioInputTokens  int64
	AudioOutputTokens int64
	ImageInputUnits   int64
	ImageOutputUnits  int64
	ToolCalls         int64
	Source            UsageSource
	Estimated         bool
	EstimationMethod  string
}

// HasImages reports whether any chat message carries an image content part.
// Multimodal requests are never cached: the cache key would have to embed
// image bytes and semantics depend on the decoded image, not just the URI.
func (u UnifiedRequest) HasImages() bool {
	if u.Chat == nil {
		return false
	}
	for _, message := range u.Chat.Messages {
		if message.HasImages() {
			return true
		}
	}
	return false
}

// TotalTokens returns normalized text/reasoning input plus output usage.
func (u UnifiedUsage) TotalTokens() int64 {
	return u.InputTokens + u.OutputTokens
}

// ToolCalls reports whether the request involves function calling (declared
// tools, echoed tool calls, or tool results). Such requests are never cached
// by default: the response depends on client-defined tools and the model may
// answer with calls instead of text.
func (u UnifiedRequest) ToolCalls() bool {
	if u.Kind == RequestTool || u.Tool != nil {
		return true
	}
	if u.Chat == nil {
		return false
	}
	if len(u.Chat.Tools) > 0 {
		return true
	}
	for _, message := range u.Chat.Messages {
		if message.Role == "tool" || strings.HasPrefix(message.Role, "tool_") || len(message.ToolCalls) > 0 {
			return true
		}
	}
	return false
}
