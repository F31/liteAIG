// Package openai translates OpenAI wire DTOs to canonical model interactions.
package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
	"time"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

// Message is the OpenAI chat wire message. Content is a plain text string for
// text-only messages or an array of {"type":"text"|"image_url",...} parts for
// multimodal messages, matching the /v1/chat/completions contract.
type Message struct {
	Role       string                 `json:"role"`
	Content    any                    `json:"content"`
	ToolCallID string                 `json:"tool_call_id,omitempty"`
	ToolCalls  []interaction.ToolCall `json:"tool_calls,omitempty"`
}
type ChatRequest struct {
	Model          string             `json:"model"`
	Messages       []Message          `json:"messages"`
	Stream         bool               `json:"stream,omitempty"`
	MaxTokens      *int               `json:"max_tokens,omitempty"`
	Temperature    *float64           `json:"temperature,omitempty"`
	TopP           *float64           `json:"top_p,omitempty"`
	Stop           StringList         `json:"stop,omitempty"`
	Tools          []interaction.Tool `json:"tools,omitempty"`
	ToolChoice     json.RawMessage    `json:"tool_choice,omitempty"`
	ResponseFormat json.RawMessage    `json:"response_format,omitempty"`
}
type StringList []string

func (s *StringList) UnmarshalJSON(data []byte) error {
	var one string
	if json.Unmarshal(data, &one) == nil {
		*s = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*s = many
	return nil
}

type Usage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}
type ChatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Created int64  `json:"created"`
	Choices []struct {
		Index        int     `json:"index"`
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}
type EmbeddingRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}
type EmbeddingResponse struct {
	Model string `json:"model"`
	Data  []struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Usage Usage `json:"usage"`
}
type RerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      *int     `json:"top_n,omitempty"`
}
type RerankResponse struct {
	Model   string `json:"model"`
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
		Document       any     `json:"document,omitempty"`
	} `json:"results"`
	Usage Usage `json:"usage,omitempty"`
}
type AudioTranscriptionResponse struct {
	Text string `json:"text"`
}
type BatchCreateRequest struct {
	Model            string            `json:"model,omitempty"`
	InputFileID      string            `json:"input_file_id"`
	Endpoint         string            `json:"endpoint"`
	CompletionWindow string            `json:"completion_window"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}
type BatchResponse struct {
	ID               string            `json:"id"`
	Object           string            `json:"object,omitempty"`
	Endpoint         string            `json:"endpoint,omitempty"`
	InputFileID      string            `json:"input_file_id,omitempty"`
	OutputFileID     string            `json:"output_file_id,omitempty"`
	ErrorFileID      string            `json:"error_file_id,omitempty"`
	Status           string            `json:"status,omitempty"`
	CompletionWindow string            `json:"completion_window,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}
type BatchListResponse struct {
	Object  string          `json:"object"`
	Data    []BatchResponse `json:"data"`
	HasMore bool            `json:"has_more"`
	FirstID string          `json:"first_id,omitempty"`
	LastID  string          `json:"last_id,omitempty"`
}
type FileResponse struct {
	ID        string `json:"id"`
	Object    string `json:"object,omitempty"`
	Bytes     int64  `json:"bytes,omitempty"`
	CreatedAt int64  `json:"created_at,omitempty"`
	Filename  string `json:"filename,omitempty"`
	Purpose   string `json:"purpose,omitempty"`
	Deleted   bool   `json:"deleted,omitempty"`
}
type FileListResponse struct {
	Object  string         `json:"object"`
	Data    []FileResponse `json:"data"`
	HasMore bool           `json:"has_more"`
}
type streamToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type StreamResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Content   string                `json:"content"`
			ToolCalls []streamToolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage,omitempty"`
}

// contentToParts projects an OpenAI wire content value (a plain string or an
// array of {"type":"text"} / {"type":"image_url"} parts) onto the canonical
// message fields. Text parts are concatenated into the returned text (canonical
// text lives only in Message.Content); image parts become ordered ContentParts
// with an inline DataURL when the client sent a data: URI (media type parsed
// from the URI prefix) or a remote URL otherwise. Image payloads are client
// content and never appear in any returned error.
func contentToParts(value any) (string, []interaction.ContentPart, error) {
	switch content := value.(type) {
	case nil:
		return "", nil, nil
	case string:
		return content, nil, nil
	case []any:
		var text strings.Builder
		var parts []interaction.ContentPart
		for index, entry := range content {
			object, ok := entry.(map[string]any)
			if !ok {
				return "", nil, invalidContentf("message content entry %d must be an object", index)
			}
			kind, _ := object["type"].(string)
			if kind == "" {
				return "", nil, invalidContentf("message content entry %d is missing a type", index)
			}
			switch kind {
			// "text"/"input_text" and "image_url"/"input_image" are the chat and
			// Responses API spellings of the same part types; both are accepted
			// so the gateway decodes either client idiom.
			case "text", "input_text":
				body, ok := object["text"].(string)
				if !ok {
					return "", nil, invalidContentf("text content entry %d is missing its text", index)
				}
				text.WriteString(body)
			case "image_url", "input_image":
				spec, ok := object["image_url"].(map[string]any)
				if !ok {
					return "", nil, invalidContentf("image content entry %d must be an object", index)
				}
				location, _ := spec["url"].(string)
				if location == "" {
					return "", nil, invalidContentf("image content entry %d is missing its url", index)
				}
				parts = append(parts, imageContentPart(location))
			default:
				// Unknown part types are ignored for forward compatibility.
			}
		}
		return text.String(), parts, nil
	default:
		return "", nil, invalidContentf("message content must be a string or an array of parts")
	}
}

// imageContentPart maps one OpenAI image_url location onto the canonical image
// part. A data: URI keeps the inline payload (media type parsed from the URI
// prefix); any other reference is treated as a remote URL that passes through
// on egress without being fetched.
func imageContentPart(location string) interaction.ContentPart {
	part := interaction.ContentPart{Kind: interaction.ContentImage}
	if strings.HasPrefix(location, "data:") {
		part.DataURL = location
		part.MediaType = dataMediaType(location)
	} else {
		part.URL = location
	}
	return part
}

// dataMediaType extracts the media type between "data:" and the first ";" or
// "," of a data: URI (data:<media-type>;base64,<payload>), left empty when the
// URI is malformed or carries no explicit type.
func dataMediaType(location string) string {
	rest := strings.TrimPrefix(location, "data:")
	if end := strings.IndexAny(rest, ";,"); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// DecodeChatBytes parses a buffered OpenAI chat body in a single pass: one
// full decode into raw fields, then targeted unmarshals of the known keys.
// It avoids the whole-body re-marshal + second decode of the reader wrapper.
func DecodeChatBytes(body []byte) (*interaction.UnifiedRequest, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode OpenAI chat request: %w", err)
	}
	var wire ChatRequest
	if value, ok := raw["model"]; ok {
		if err := json.Unmarshal(value, &wire.Model); err != nil {
			return nil, err
		}
	}
	if value, ok := raw["messages"]; ok {
		if err := json.Unmarshal(value, &wire.Messages); err != nil {
			return nil, err
		}
	}
	if value, ok := raw["stream"]; ok {
		if err := json.Unmarshal(value, &wire.Stream); err != nil {
			return nil, err
		}
	}
	if value, ok := raw["max_tokens"]; ok {
		if err := json.Unmarshal(value, &wire.MaxTokens); err != nil {
			return nil, err
		}
	}
	if value, ok := raw["temperature"]; ok {
		if err := json.Unmarshal(value, &wire.Temperature); err != nil {
			return nil, err
		}
	}
	if value, ok := raw["top_p"]; ok {
		if err := json.Unmarshal(value, &wire.TopP); err != nil {
			return nil, err
		}
	}
	if value, ok := raw["stop"]; ok {
		if err := json.Unmarshal(value, &wire.Stop); err != nil {
			return nil, err
		}
	}
	if value, ok := raw["tools"]; ok {
		if err := json.Unmarshal(value, &wire.Tools); err != nil {
			return nil, fmt.Errorf("decode tools: %w", err)
		}
	}
	if value, ok := raw["tool_choice"]; ok {
		wire.ToolChoice = value
	}
	if value, ok := raw["response_format"]; ok {
		wire.ResponseFormat = value
	}
	if wire.Model == "" || len(wire.Messages) == 0 {
		return nil, fmt.Errorf("model and messages are required")
	}
	extras := extras(raw, "model", "messages", "stream", "max_tokens", "temperature", "top_p", "stop", "tools", "tool_choice", "response_format")
	messages := make([]interaction.Message, len(wire.Messages))
	for i, item := range wire.Messages {
		text, parts, err := contentToParts(item.Content)
		if err != nil {
			return nil, err
		}
		messages[i] = interaction.Message{Role: item.Role, Content: text, Parts: parts, ToolCallID: item.ToolCallID, ToolCalls: item.ToolCalls}
	}
	return &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: wire.Model, Stream: wire.Stream, Chat: &interaction.ChatPayload{Messages: messages, MaxOutputTokens: wire.MaxTokens, Temperature: wire.Temperature, TopP: wire.TopP, Stop: wire.Stop, Tools: wire.Tools, ToolChoice: wire.ToolChoice, ResponseFormat: wire.ResponseFormat}, Parameters: extras}, nil
}

func DecodeChat(reader io.Reader) (*interaction.UnifiedRequest, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return DecodeChatBytes(body)
}

func EncodeChat(request *interaction.UnifiedRequest) ([]byte, error) {
	if request == nil || request.Chat == nil {
		return nil, fmt.Errorf("chat payload is required")
	}
	messages := make([]Message, len(request.Chat.Messages))
	for i, item := range request.Chat.Messages {
		messages[i] = Message{Role: item.Role, Content: contentValue(item.Content, item.Parts), ToolCallID: item.ToolCallID, ToolCalls: item.ToolCalls}
	}
	wire := map[string]any{"model": request.Model, "messages": messages, "stream": request.Stream}
	if request.Chat.MaxOutputTokens != nil {
		wire["max_tokens"] = *request.Chat.MaxOutputTokens
	}
	if request.Chat.Temperature != nil {
		wire["temperature"] = *request.Chat.Temperature
	}
	if request.Chat.TopP != nil {
		wire["top_p"] = *request.Chat.TopP
	}
	if len(request.Chat.Stop) > 0 {
		wire["stop"] = request.Chat.Stop
	}
	if len(request.Chat.Tools) > 0 {
		wire["tools"] = request.Chat.Tools
	}
	if len(request.Chat.ToolChoice) > 0 {
		var choice any
		if err := json.Unmarshal(request.Chat.ToolChoice, &choice); err == nil {
			wire["tool_choice"] = choice
		}
	}
	if len(request.Chat.ResponseFormat) > 0 {
		var format any
		if err := json.Unmarshal(request.Chat.ResponseFormat, &format); err == nil {
			wire["response_format"] = format
		}
	}
	for name, value := range request.Parameters {
		var decoded any
		if err := json.Unmarshal(value, &decoded); err != nil {
			return nil, err
		}
		wire[name] = decoded
	}
	return json.Marshal(wire)
}

func DecodeChatResponse(reader io.Reader) (*interaction.UnifiedResponse, error) {
	var wire ChatResponse
	if err := json.NewDecoder(reader).Decode(&wire); err != nil {
		return nil, err
	}
	result := &interaction.UnifiedResponse{ID: wire.ID, Model: wire.Model, CreatedAt: time.Unix(wire.Created, 0), Usage: toUnifiedUsage(wire.Usage)}
	for _, item := range wire.Choices {
		content, _, err := contentToParts(item.Message.Content)
		if err != nil {
			return nil, err
		}
		result.Choices = append(result.Choices, interaction.Choice{Index: item.Index, Message: interaction.Message{Role: item.Message.Role, Content: content, ToolCalls: item.Message.ToolCalls}})
		if item.FinishReason != "" {
			result.StopReason = item.FinishReason
		}
	}
	return result, nil
}

func DecodeEmbeddingBytes(body []byte) (*interaction.UnifiedRequest, error) {
	var wire EmbeddingRequest
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}
	var inputs []string
	switch value := wire.Input.(type) {
	case string:
		inputs = []string{value}
	case []any:
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("embedding input must be text")
			}
			inputs = append(inputs, text)
		}
	default:
		return nil, fmt.Errorf("embedding input is required")
	}
	return &interaction.UnifiedRequest{Kind: interaction.RequestEmbedding, Model: wire.Model, Embedding: &interaction.EmbeddingPayload{Inputs: inputs}}, nil
}

func DecodeRerankBytes(body []byte) (*interaction.UnifiedRequest, error) {
	var wire RerankRequest
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}
	if wire.Model == "" || wire.Query == "" || len(wire.Documents) == 0 {
		return nil, fmt.Errorf("rerank model, query, and documents are required")
	}
	return &interaction.UnifiedRequest{Kind: interaction.RequestRerank, Model: wire.Model, Rerank: &interaction.RerankPayload{Query: wire.Query, Documents: wire.Documents, TopN: wire.TopN}}, nil
}

func DecodeRerank(reader io.Reader) (*interaction.UnifiedRequest, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return DecodeRerankBytes(body)
}

func EncodeRerank(request *interaction.UnifiedRequest) ([]byte, error) {
	if request == nil || request.Rerank == nil {
		return nil, fmt.Errorf("rerank payload is required")
	}
	return json.Marshal(RerankRequest{Model: request.Model, Query: request.Rerank.Query, Documents: request.Rerank.Documents, TopN: request.Rerank.TopN})
}

func DecodeRerankResponse(reader io.Reader) (*interaction.UnifiedResponse, error) {
	var wire RerankResponse
	if err := json.NewDecoder(reader).Decode(&wire); err != nil {
		return nil, err
	}
	result := &interaction.UnifiedResponse{Model: wire.Model, Usage: toUnifiedUsage(wire.Usage)}
	for _, item := range wire.Results {
		document := ""
		switch value := item.Document.(type) {
		case string:
			document = value
		case map[string]any:
			if text, ok := value["text"].(string); ok {
				document = text
			}
		}
		result.Rerank = append(result.Rerank, interaction.RerankResult{Index: item.Index, RelevanceScore: item.RelevanceScore, Document: document})
	}
	return result, nil
}

func EncodeRerankResponse(response *interaction.UnifiedResponse, model string) ([]byte, error) {
	if response == nil {
		response = &interaction.UnifiedResponse{}
	}
	wire := RerankResponse{Model: valueOr(response.Model, model), Usage: Usage{PromptTokens: response.Usage.InputTokens, CompletionTokens: response.Usage.OutputTokens, TotalTokens: response.Usage.TotalTokens()}}
	for _, item := range response.Rerank {
		wire.Results = append(wire.Results, struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
			Document       any     `json:"document,omitempty"`
		}{Index: item.Index, RelevanceScore: item.RelevanceScore, Document: item.Document})
	}
	return json.Marshal(wire)
}

func DecodeAudioTranscription(contentType string, reader io.Reader) (*interaction.UnifiedRequest, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") || params["boundary"] == "" {
		return nil, fmt.Errorf("audio transcription requires multipart form-data")
	}
	form, err := multipart.NewReader(reader, params["boundary"]).ReadForm(32 << 20)
	if err != nil {
		return nil, err
	}
	defer form.RemoveAll()
	model := firstFormValue(form, "model")
	files := form.File["file"]
	if model == "" || len(files) == 0 {
		return nil, fmt.Errorf("audio transcription model and file are required")
	}
	file, err := files[0].Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	temp, err := optionalFloat(form, "temperature")
	if err != nil {
		return nil, err
	}
	return &interaction.UnifiedRequest{Kind: interaction.RequestAudio, Model: model, Audio: &interaction.AudioPayload{Filename: files[0].Filename, MediaType: files[0].Header.Get("Content-Type"), Data: data, Language: firstFormValue(form, "language"), Prompt: firstFormValue(form, "prompt"), ResponseFormat: firstFormValue(form, "response_format"), Temperature: temp}}, nil
}

func EncodeAudioTranscription(request *interaction.UnifiedRequest) ([]byte, string, error) {
	if request == nil || request.Audio == nil {
		return nil, "", fmt.Errorf("audio transcription payload is required")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", request.Model); err != nil {
		return nil, "", err
	}
	writeOptionalField(writer, "language", request.Audio.Language)
	writeOptionalField(writer, "prompt", request.Audio.Prompt)
	writeOptionalField(writer, "response_format", request.Audio.ResponseFormat)
	if request.Audio.Temperature != nil {
		writeOptionalField(writer, "temperature", fmt.Sprintf("%g", *request.Audio.Temperature))
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(valueOr(request.Audio.Filename, "audio"))))
	if request.Audio.MediaType != "" {
		header.Set("Content-Type", request.Audio.MediaType)
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(request.Audio.Data); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

func DecodeAudioTranscriptionResponse(reader io.Reader) (*interaction.UnifiedResponse, error) {
	var wire AudioTranscriptionResponse
	if err := json.NewDecoder(reader).Decode(&wire); err != nil {
		return nil, err
	}
	return &interaction.UnifiedResponse{Audio: &interaction.AudioResult{Text: wire.Text}, Choices: []interaction.Choice{{Index: 0, Message: interaction.Message{Role: "assistant", Content: wire.Text}}}}, nil
}

func EncodeAudioTranscriptionResponse(response *interaction.UnifiedResponse) ([]byte, error) {
	text := ""
	if response != nil && response.Audio != nil {
		text = response.Audio.Text
	} else if response != nil && len(response.Choices) > 0 {
		text = response.Choices[0].Message.Content
	}
	return json.Marshal(AudioTranscriptionResponse{Text: text})
}

func DecodeBatchCreateBytes(body []byte) (*interaction.UnifiedRequest, error) {
	var wire BatchCreateRequest
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}
	if wire.Model == "" || wire.InputFileID == "" || wire.Endpoint == "" || wire.CompletionWindow == "" {
		return nil, fmt.Errorf("batch create model, input_file_id, endpoint, and completion_window are required")
	}
	return &interaction.UnifiedRequest{Kind: interaction.RequestBatch, Model: wire.Model, Batch: &interaction.BatchPayload{Operation: "create", InputFileID: wire.InputFileID, Endpoint: wire.Endpoint, CompletionWindow: wire.CompletionWindow, Metadata: wire.Metadata}}, nil
}

func NewBatchLifecycleRequest(operation, model, id string, limit int, after string) *interaction.UnifiedRequest {
	return &interaction.UnifiedRequest{Kind: interaction.RequestBatch, Model: model, Batch: &interaction.BatchPayload{Operation: operation, ID: id, Limit: limit, After: after}}
}

func NewFileContentRequest(model, id string) *interaction.UnifiedRequest {
	return &interaction.UnifiedRequest{Kind: interaction.RequestFile, Model: model, File: &interaction.FilePayload{Operation: "content", ID: id}}
}

func NewFileLifecycleRequest(operation, model, id string, limit int, after string) *interaction.UnifiedRequest {
	return &interaction.UnifiedRequest{Kind: interaction.RequestFile, Model: model, File: &interaction.FilePayload{Operation: operation, ID: id, Limit: limit, After: after}}
}

func EncodeBatchCreate(request *interaction.UnifiedRequest) ([]byte, error) {
	if request == nil || request.Batch == nil {
		return nil, fmt.Errorf("batch create payload is required")
	}
	return json.Marshal(BatchCreateRequest{InputFileID: request.Batch.InputFileID, Endpoint: request.Batch.Endpoint, CompletionWindow: request.Batch.CompletionWindow, Metadata: request.Batch.Metadata})
}

func DecodeBatchResponseBytes(body []byte) (*interaction.UnifiedResponse, error) {
	return DecodeBatchResponse(bytes.NewReader(body))
}

func DecodeBatchResponse(reader io.Reader) (*interaction.UnifiedResponse, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var list BatchListResponse
	if err := json.Unmarshal(body, &list); err == nil && list.Object == "list" {
		result := &interaction.BatchResult{Object: list.Object, HasMore: list.HasMore, FirstID: list.FirstID, LastID: list.LastID, Raw: append([]byte(nil), body...)}
		for _, item := range list.Data {
			result.Items = append(result.Items, interaction.BatchResult{ID: item.ID, Object: item.Object, Endpoint: item.Endpoint, InputFileID: item.InputFileID, OutputFileID: item.OutputFileID, ErrorFileID: item.ErrorFileID, Status: item.Status, CompletionWindow: item.CompletionWindow})
		}
		return &interaction.UnifiedResponse{Batch: result}, nil
	}
	var wire BatchResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}
	return &interaction.UnifiedResponse{ID: wire.ID, Batch: &interaction.BatchResult{ID: wire.ID, Object: wire.Object, Endpoint: wire.Endpoint, InputFileID: wire.InputFileID, OutputFileID: wire.OutputFileID, ErrorFileID: wire.ErrorFileID, Status: wire.Status, CompletionWindow: wire.CompletionWindow, Raw: append([]byte(nil), body...)}}, nil
}

func EncodeBatchResponse(response *interaction.UnifiedResponse) ([]byte, error) {
	if response != nil && response.Batch != nil && len(response.Batch.Raw) > 0 {
		return response.Batch.Raw, nil
	}
	if response == nil || response.Batch == nil {
		return json.Marshal(BatchResponse{})
	}
	if len(response.Batch.Items) > 0 || response.Batch.Object == "list" {
		items := make([]BatchResponse, 0, len(response.Batch.Items))
		for _, item := range response.Batch.Items {
			items = append(items, BatchResponse{ID: item.ID, Object: item.Object, Endpoint: item.Endpoint, InputFileID: item.InputFileID, OutputFileID: item.OutputFileID, ErrorFileID: item.ErrorFileID, Status: item.Status, CompletionWindow: item.CompletionWindow})
		}
		return json.Marshal(BatchListResponse{Object: "list", Data: items, HasMore: response.Batch.HasMore, FirstID: response.Batch.FirstID, LastID: response.Batch.LastID})
	}
	return json.Marshal(BatchResponse{ID: response.Batch.ID, Object: response.Batch.Object, Endpoint: response.Batch.Endpoint, InputFileID: response.Batch.InputFileID, OutputFileID: response.Batch.OutputFileID, ErrorFileID: response.Batch.ErrorFileID, Status: response.Batch.Status, CompletionWindow: response.Batch.CompletionWindow})
}

func DecodeFileContentResponse(reader io.Reader) (*interaction.UnifiedResponse, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return &interaction.UnifiedResponse{RawContent: body}, nil
}

func DecodeFileUpload(contentType string, reader io.Reader) (*interaction.UnifiedRequest, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") || params["boundary"] == "" {
		return nil, fmt.Errorf("file upload requires multipart form-data")
	}
	form, err := multipart.NewReader(reader, params["boundary"]).ReadForm(32 << 20)
	if err != nil {
		return nil, err
	}
	defer form.RemoveAll()
	model := firstFormValue(form, "model")
	purpose := firstFormValue(form, "purpose")
	files := form.File["file"]
	if model == "" || purpose == "" || len(files) == 0 {
		return nil, fmt.Errorf("file upload model, purpose, and file are required")
	}
	file, err := files[0].Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return &interaction.UnifiedRequest{Kind: interaction.RequestFile, Model: model, File: &interaction.FilePayload{Operation: "upload", Purpose: purpose, Filename: files[0].Filename, MediaType: files[0].Header.Get("Content-Type"), Data: data}}, nil
}

func EncodeFileUpload(request *interaction.UnifiedRequest) ([]byte, string, error) {
	if request == nil || request.File == nil || request.File.Purpose == "" {
		return nil, "", fmt.Errorf("file upload payload is required")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("purpose", request.File.Purpose); err != nil {
		return nil, "", err
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(valueOr(request.File.Filename, "file"))))
	if request.File.MediaType != "" {
		header.Set("Content-Type", request.File.MediaType)
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(request.File.Data); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

func DecodeFileResponse(reader io.Reader) (*interaction.UnifiedResponse, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var list FileListResponse
	if err := json.Unmarshal(body, &list); err == nil && list.Object == "list" {
		result := &interaction.FileResult{Object: list.Object, HasMore: list.HasMore, Raw: append([]byte(nil), body...)}
		for _, item := range list.Data {
			result.Items = append(result.Items, interaction.FileResult{ID: item.ID, Object: item.Object, Bytes: item.Bytes, CreatedAt: item.CreatedAt, Filename: item.Filename, Purpose: item.Purpose, Deleted: item.Deleted})
		}
		return &interaction.UnifiedResponse{File: result}, nil
	}
	var wire FileResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}
	return &interaction.UnifiedResponse{ID: wire.ID, File: &interaction.FileResult{ID: wire.ID, Object: wire.Object, Bytes: wire.Bytes, CreatedAt: wire.CreatedAt, Filename: wire.Filename, Purpose: wire.Purpose, Deleted: wire.Deleted, Raw: append([]byte(nil), body...)}}, nil
}

func EncodeFileResponse(response *interaction.UnifiedResponse) ([]byte, error) {
	if response != nil && response.File != nil && len(response.File.Raw) > 0 {
		return response.File.Raw, nil
	}
	if response == nil || response.File == nil {
		return json.Marshal(FileResponse{})
	}
	if len(response.File.Items) > 0 || response.File.Object == "list" {
		items := make([]FileResponse, 0, len(response.File.Items))
		for _, item := range response.File.Items {
			items = append(items, FileResponse{ID: item.ID, Object: item.Object, Bytes: item.Bytes, CreatedAt: item.CreatedAt, Filename: item.Filename, Purpose: item.Purpose, Deleted: item.Deleted})
		}
		return json.Marshal(FileListResponse{Object: "list", Data: items, HasMore: response.File.HasMore})
	}
	return json.Marshal(FileResponse{ID: response.File.ID, Object: response.File.Object, Bytes: response.File.Bytes, CreatedAt: response.File.CreatedAt, Filename: response.File.Filename, Purpose: response.File.Purpose, Deleted: response.File.Deleted})
}

func firstFormValue(form *multipart.Form, name string) string {
	if values := form.Value[name]; len(values) > 0 {
		return values[0]
	}
	return ""
}

func optionalFloat(form *multipart.Form, name string) (*float64, error) {
	value := firstFormValue(form, name)
	if value == "" {
		return nil, nil
	}
	var out float64
	if _, err := fmt.Sscan(value, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func writeOptionalField(writer *multipart.Writer, name, value string) {
	if value != "" {
		_ = writer.WriteField(name, value)
	}
}

func escapeQuotes(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}

func DecodeEmbedding(reader io.Reader) (*interaction.UnifiedRequest, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return DecodeEmbeddingBytes(body)
}

func EncodeEmbedding(request *interaction.UnifiedRequest) ([]byte, error) {
	if request == nil || request.Embedding == nil {
		return nil, fmt.Errorf("embedding payload is required")
	}
	return json.Marshal(map[string]any{"model": request.Model, "input": request.Embedding.Inputs})
}
func DecodeEmbeddingResponse(reader io.Reader) (*interaction.UnifiedResponse, error) {
	var wire EmbeddingResponse
	if err := json.NewDecoder(reader).Decode(&wire); err != nil {
		return nil, err
	}
	result := &interaction.UnifiedResponse{Model: wire.Model, Usage: toUnifiedUsage(wire.Usage)}
	for _, item := range wire.Data {
		result.Embeddings = append(result.Embeddings, item.Embedding)
	}
	return result, nil
}
func NormalizeStream(data []byte) (interaction.StreamEvent, error) {
	var wire StreamResponse
	if err := json.Unmarshal(data, &wire); err != nil {
		return interaction.StreamEvent{}, err
	}
	event := interaction.StreamEvent{ID: wire.ID, Model: wire.Model}
	if len(wire.Choices) > 0 {
		event.Index = wire.Choices[0].Index
		event.Delta = wire.Choices[0].Delta.Content
		event.StopReason = wire.Choices[0].FinishReason
		event.Final = event.StopReason != ""
		for _, item := range wire.Choices[0].Delta.ToolCalls {
			event.ToolCallDeltas = append(event.ToolCallDeltas, interaction.ToolCallDelta{Index: item.Index, ID: item.ID, Type: item.Type, Name: item.Function.Name, Arguments: item.Function.Arguments})
		}
	}
	if wire.Usage != nil {
		usage := toUnifiedUsage(*wire.Usage)
		event.Usage = &usage
	}
	return event, nil
}
func extras(raw map[string]json.RawMessage, known ...string) map[string]json.RawMessage {
	for _, key := range known {
		delete(raw, key)
	}
	return raw
}

// toUnifiedUsage maps provider-reported usage to the normalized fact, tagging
// the source so accounting can distinguish it from local estimates.
func toUnifiedUsage(usage Usage) interaction.UnifiedUsage {
	return interaction.UnifiedUsage{
		InputTokens:       usage.PromptTokens,
		OutputTokens:      usage.CompletionTokens,
		CacheReadTokens:   usage.PromptTokensDetails.CachedTokens,
		CachedInputTokens: usage.PromptTokensDetails.CachedTokens,
		ReasoningTokens:   usage.CompletionTokensDetails.ReasoningTokens,
		Source:            interaction.UsageProvider,
	}
}
