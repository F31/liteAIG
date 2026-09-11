// Package anthropic translates Anthropic Messages wire DTOs to canonical interactions.
package anthropic

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

// contentBlock is the Anthropic content-block wire shape, covering text,
// tool_use, tool_result, and image blocks.
type contentBlock struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	ID           string          `json:"id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	Content      any             `json:"content,omitempty"`
	IsError      bool            `json:"is_error,omitempty"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
	Source       *imageSource    `json:"source,omitempty"`
}

// imageSource is the Anthropic image content-block source. Inline base64
// sources are the only form the Messages API accepts; url sources are decoded
// but rejected on encode (the gateway never fetches remote images).
type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// cacheControl carries Anthropic's explicit prompt-cache marker. "ephemeral"
// is the only type Anthropic supports today.
type cacheControl struct {
	Type string `json:"type"`
}

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}
type Request struct {
	Model         string    `json:"model"`
	System        any       `json:"system,omitempty"` // string or content-block array
	Messages      []Message `json:"messages"`
	MaxTokens     int       `json:"max_tokens"`
	Stream        bool      `json:"stream,omitempty"`
	Temperature   *float64  `json:"temperature,omitempty"`
	TopP          *float64  `json:"top_p,omitempty"`
	StopSequences []string  `json:"stop_sequences,omitempty"`
	Tools         []struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		InputSchema json.RawMessage `json:"input_schema,omitempty"`
	} `json:"tools,omitempty"`
	ToolChoice json.RawMessage `json:"tool_choice,omitempty"`
}
type Response struct {
	ID         string         `json:"id"`
	Model      string         `json:"model"`
	Content    []contentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	} `json:"usage"`
}
type StreamResponse struct {
	Type         string        `json:"type"`
	Index        int           `json:"index"`
	ContentBlock *contentBlock `json:"content_block,omitempty"`
	Delta        struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	} `json:"usage,omitempty"`
}

// DecodeMessagesBytes parses a buffered Anthropic body in a single pass: one
// full decode into raw fields, then targeted unmarshals of the known keys.
func DecodeMessagesBytes(body []byte) (*interaction.UnifiedRequest, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode Anthropic request: %w", err)
	}
	var wire Request
	if value, ok := raw["model"]; ok {
		if err := json.Unmarshal(value, &wire.Model); err != nil {
			return nil, err
		}
	}
	if value, ok := raw["system"]; ok {
		if err := json.Unmarshal(value, &wire.System); err != nil {
			return nil, err
		}
	}
	if value, ok := raw["messages"]; ok {
		if err := json.Unmarshal(value, &wire.Messages); err != nil {
			return nil, err
		}
	}
	if value, ok := raw["max_tokens"]; ok {
		if err := json.Unmarshal(value, &wire.MaxTokens); err != nil {
			return nil, err
		}
	}
	if value, ok := raw["stream"]; ok {
		if err := json.Unmarshal(value, &wire.Stream); err != nil {
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
	if value, ok := raw["stop_sequences"]; ok {
		if err := json.Unmarshal(value, &wire.StopSequences); err != nil {
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
	if wire.Model == "" || wire.MaxTokens <= 0 || len(wire.Messages) == 0 {
		return nil, fmt.Errorf("model, max_tokens, and messages are required")
	}
	messages := make([]interaction.Message, 0, len(wire.Messages)+1)
	if system := decodeSystem(wire.System); system != nil {
		messages = append(messages, *system)
	}
	for _, item := range wire.Messages {
		messages = append(messages, decodeMessage(item)...)
	}
	tools := make([]interaction.Tool, 0, len(wire.Tools))
	for _, item := range wire.Tools {
		tools = append(tools, interaction.Tool{Type: "function", Function: interaction.ToolFunction{Name: item.Name, Description: item.Description, Parameters: item.InputSchema}})
	}
	for _, key := range []string{"model", "system", "messages", "max_tokens", "stream", "temperature", "top_p", "stop_sequences", "tools", "tool_choice"} {
		delete(raw, key)
	}
	max := wire.MaxTokens
	return &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: wire.Model, Stream: wire.Stream, Chat: &interaction.ChatPayload{Messages: messages, MaxOutputTokens: &max, Temperature: wire.Temperature, TopP: wire.TopP, Stop: wire.StopSequences, Tools: tools, ToolChoice: wire.ToolChoice}, Parameters: raw}, nil
}

// decodeSystem flattens the Anthropic system prompt (a plain string or a
// content-block array) into a canonical system message, forwarding an explicit
// cache_control marker from the first text block.
func decodeSystem(value any) *interaction.Message {
	switch value := value.(type) {
	case string:
		if value == "" {
			return nil
		}
		return &interaction.Message{Role: "system", Content: value}
	case []any:
		var content string
		cache := ""
		for _, raw := range value {
			var block contentBlock
			if err := json.Unmarshal(mustMarshal(raw), &block); err != nil || block.Type != "text" {
				continue
			}
			if content != "" {
				content += "\n"
			}
			content += block.Text
			if block.CacheControl != nil && block.CacheControl.Type != "" {
				cache = block.CacheControl.Type
			}
		}
		if content == "" {
			return nil
		}
		return &interaction.Message{Role: "system", Content: content, CacheControl: cache}
	default:
		return nil
	}
}

// decodeMessage flattens one Anthropic message (string or content-block
// array) into canonical messages. A tool_result block becomes its own
// role:"tool" message; assistant tool_use blocks attach to the assistant
// message as ToolCalls; image blocks become ordered ContentParts (text stays
// concatenated in Content). An explicit cache_control marker on a text block
// is forwarded onto the canonical message so the gateway can pass it back to
// Anthropic on re-encode (prompt-cache pass-through, §11.3).
func decodeMessage(item Message) []interaction.Message {
	text, _ := item.Content.(string)
	blocks, isBlocks := item.Content.([]any)
	if !isBlocks {
		return []interaction.Message{{Role: item.Role, Content: text}}
	}
	var out []interaction.Message
	current := interaction.Message{Role: item.Role}
	flush := func() {
		if current.Role != "" {
			out = append(out, current)
		}
		current = interaction.Message{Role: item.Role}
	}
	for _, raw := range blocks {
		var block contentBlock
		if err := json.Unmarshal(mustMarshal(raw), &block); err != nil {
			continue
		}
		switch block.Type {
		case "text":
			if current.Content != "" {
				current.Content += "\n"
			}
			current.Content += block.Text
			if block.CacheControl != nil && block.CacheControl.Type != "" {
				current.CacheControl = block.CacheControl.Type
			}
		case "tool_use":
			current.ToolCalls = append(current.ToolCalls, interaction.ToolCall{ID: block.ID, Type: "function", Function: interaction.ToolCallFunction{Name: block.Name, Arguments: string(block.Input)}})
		case "image":
			if block.Source == nil {
				continue
			}
			switch block.Source.Type {
			case "base64":
				current.Parts = append(current.Parts, interaction.ContentPart{Kind: interaction.ContentImage, MediaType: block.Source.MediaType, DataURL: "data:" + block.Source.MediaType + ";base64," + block.Source.Data})
			case "url":
				current.Parts = append(current.Parts, interaction.ContentPart{Kind: interaction.ContentImage, URL: block.Source.URL})
			}
		case "tool_result":
			flush()
			out = append(out, interaction.Message{Role: "tool", ToolCallID: block.ToolUseID, Content: toolResultText(block)})
			current = interaction.Message{Role: item.Role}
		}
	}
	flush()
	return out
}

func toolResultText(block contentBlock) string {
	if text, ok := block.Content.(string); ok {
		return text
	}
	if list, ok := block.Content.([]any); ok {
		var parts []string
		for _, raw := range list {
			var part contentBlock
			if err := json.Unmarshal(mustMarshal(raw), &part); err == nil && part.Type == "text" {
				parts = append(parts, part.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func mustMarshal(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}

func DecodeMessages(reader io.Reader) (*interaction.UnifiedRequest, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return DecodeMessagesBytes(body)
}

func EncodeMessages(request *interaction.UnifiedRequest) ([]byte, error) {
	if request == nil || request.Chat == nil || request.Chat.MaxOutputTokens == nil {
		return nil, fmt.Errorf("Anthropic chat and max tokens are required")
	}
	wire := map[string]any{"model": request.Model, "max_tokens": *request.Chat.MaxOutputTokens, "stream": request.Stream}
	var messages []Message
	for _, item := range request.Chat.Messages {
		if item.Role == "system" {
			if item.CacheControl != "" {
				// A cache-marked system prompt must be sent as a content-block
				// array (a string cannot carry cache_control).
				wire["system"] = []contentBlock{textBlock(item)}
			} else {
				wire["system"] = item.Content
			}
			continue
		}
		encoded, err := encodeMessage(item)
		if err != nil {
			return nil, err
		}
		messages = append(messages, encoded)
	}
	wire["messages"] = messages
	if len(request.Chat.Tools) > 0 {
		var tools []map[string]any
		for _, item := range request.Chat.Tools {
			entry := map[string]any{"name": item.Function.Name}
			if item.Function.Description != "" {
				entry["description"] = item.Function.Description
			}
			if len(item.Function.Parameters) > 0 {
				var schema any
				if err := json.Unmarshal(item.Function.Parameters, &schema); err == nil {
					entry["input_schema"] = schema
				}
			}
			tools = append(tools, entry)
		}
		wire["tools"] = tools
	}
	if choice, ok := anthropicToolChoice(request.Chat.ToolChoice); ok {
		wire["tool_choice"] = choice
	}
	if request.Chat.Temperature != nil {
		wire["temperature"] = *request.Chat.Temperature
	}
	if request.Chat.TopP != nil {
		wire["top_p"] = *request.Chat.TopP
	}
	if len(request.Chat.Stop) > 0 {
		wire["stop_sequences"] = request.Chat.Stop
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

// encodeMessage renders one canonical message into the Anthropic wire shape.
// Assistant tool calls become tool_use content blocks; role:"tool" messages
// become user messages carrying a tool_result block. A message with an
// explicit CacheControl marker or image Parts is emitted as a content-block
// array so the cache_control and image blocks survive to the provider
// (prompt-cache pass-through, §11.3).
func encodeMessage(item interaction.Message) (Message, error) {
	switch {
	case item.Role == "tool":
		return Message{Role: "user", Content: []contentBlock{{Type: "tool_result", ToolUseID: item.ToolCallID, Content: item.Content}}}, nil
	case len(item.ToolCalls) > 0:
		blocks, err := messageBlocks(item)
		if err != nil {
			return Message{}, err
		}
		for _, call := range item.ToolCalls {
			input := json.RawMessage("{}")
			if len([]byte(call.Function.Arguments)) > 0 {
				input = json.RawMessage(call.Function.Arguments)
			}
			blocks = append(blocks, contentBlock{Type: "tool_use", ID: call.ID, Name: call.Function.Name, Input: input})
		}
		return Message{Role: "assistant", Content: blocks}, nil
	case len(item.Parts) > 0 || (item.CacheControl != "" && item.Content != ""):
		blocks, err := messageBlocks(item)
		if err != nil {
			return Message{}, err
		}
		return Message{Role: item.Role, Content: blocks}, nil
	default:
		return Message{Role: item.Role, Content: item.Content}, nil
	}
}

// messageBlocks renders a canonical message's text block (when Content is
// non-empty) followed by its image Parts in order. Text-only, cache-free
// messages are not routed here so their wire body stays a plain string.
func messageBlocks(item interaction.Message) ([]contentBlock, error) {
	var blocks []contentBlock
	if item.Content != "" {
		blocks = append(blocks, textBlock(item))
	}
	for _, part := range item.Parts {
		block, err := imageBlock(part)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

// imageBlock renders one canonical image part as an Anthropic image content
// block. Anthropic's Messages API only accepts inline base64 sources, so a
// part carrying only a remote URL is an error (never silently dropped): the
// gateway does not fetch remote images. Image payloads are client content and
// are never embedded in the returned error.
func imageBlock(part interaction.ContentPart) (contentBlock, error) {
	if part.Kind != interaction.ContentImage || part.DataURL == "" {
		return contentBlock{}, fmt.Errorf("anthropic upstream requires inline data-URL images")
	}
	mediaType, payload, ok := splitDataURL(part.DataURL)
	if !ok {
		return contentBlock{}, fmt.Errorf("malformed image data URL")
	}
	if part.MediaType != "" {
		mediaType = part.MediaType
	}
	if mediaType == "" {
		return contentBlock{}, fmt.Errorf("image part missing media type")
	}
	return contentBlock{Type: "image", Source: &imageSource{Type: "base64", MediaType: mediaType, Data: payload}}, nil
}

// splitDataURL splits a data:<media-type>;base64,<payload> URI into its media
// type and payload. It reports ok=false for anything outside that exact shape.
func splitDataURL(dataURL string) (mediaType, payload string, ok bool) {
	const prefix = "data:"
	if !strings.HasPrefix(dataURL, prefix) {
		return "", "", false
	}
	rest := dataURL[len(prefix):]
	sep := strings.Index(rest, ";base64,")
	if sep <= 0 {
		return "", "", false
	}
	return rest[:sep], rest[sep+len(";base64,"):], true
}

// textBlock renders a plain-text message as an Anthropic text block, carrying
// the message's cache_control marker when set.
func textBlock(item interaction.Message) contentBlock {
	block := contentBlock{Type: "text", Text: item.Content}
	if item.CacheControl != "" {
		block.CacheControl = &cacheControl{Type: item.CacheControl}
	}
	return block
}

// anthropicToolChoice maps an OpenAI-style tool_choice to the Anthropic
// shape. "auto" passes through; "required" becomes "any"; a named function
// becomes a tool choice; "none" and unknown shapes are dropped.
func anthropicToolChoice(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		switch text {
		case "auto":
			return "auto", true
		case "required":
			return "any", true
		}
		return nil, false
	}
	var named struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &named); err == nil && named.Type == "function" && named.Function.Name != "" {
		return map[string]string{"type": "tool", "name": named.Function.Name}, true
	}
	// Already in the Anthropic shape ({"type":"any"} / {"type":"tool",...}).
	var passthrough any
	if err := json.Unmarshal(raw, &passthrough); err == nil {
		return passthrough, true
	}
	return nil, false
}

func DecodeResponse(reader io.Reader) (*interaction.UnifiedResponse, error) {
	var wire Response
	if err := json.NewDecoder(reader).Decode(&wire); err != nil {
		return nil, err
	}
	text := ""
	var calls []interaction.ToolCall
	for _, block := range wire.Content {
		switch block.Type {
		case "text":
			if text != "" {
				text += "\n"
			}
			text += block.Text
		case "tool_use":
			calls = append(calls, interaction.ToolCall{ID: block.ID, Type: "function", Function: interaction.ToolCallFunction{Name: block.Name, Arguments: string(block.Input)}})
		}
	}
	return &interaction.UnifiedResponse{ID: wire.ID, Model: wire.Model, StopReason: wire.StopReason, Choices: []interaction.Choice{{Index: 0, Message: interaction.Message{Role: "assistant", Content: text, ToolCalls: calls}}}, Usage: toUnifiedUsage(anthropicUsage{InputTokens: wire.Usage.InputTokens, OutputTokens: wire.Usage.OutputTokens, CacheCreationInputTokens: wire.Usage.CacheCreationInputTokens, CacheReadInputTokens: wire.Usage.CacheReadInputTokens})}, nil
}

// anthropicUsage mirrors the usage object on Response and StreamResponse.
type anthropicUsage struct {
	InputTokens              int64
	OutputTokens             int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64
}

// toUnifiedUsage maps provider-reported usage to the normalized fact, tagging
// the source so accounting can distinguish it from local estimates.
func toUnifiedUsage(usage anthropicUsage) interaction.UnifiedUsage {
	return interaction.UnifiedUsage{
		InputTokens:       usage.InputTokens,
		OutputTokens:      usage.OutputTokens,
		CacheWriteTokens:  usage.CacheCreationInputTokens,
		CacheReadTokens:   usage.CacheReadInputTokens,
		CachedInputTokens: usage.CacheReadInputTokens,
		Source:            interaction.UsageProvider,
	}
}

func NormalizeStream(data []byte) (interaction.StreamEvent, error) {
	var wire StreamResponse
	if err := json.Unmarshal(data, &wire); err != nil {
		return interaction.StreamEvent{}, err
	}
	event := interaction.StreamEvent{Index: wire.Index, Delta: wire.Delta.Text, StopReason: wire.Delta.StopReason, Final: wire.Type == "message_stop" || wire.Delta.StopReason != ""}
	switch wire.Type {
	case "content_block_start":
		if wire.ContentBlock != nil && wire.ContentBlock.Type == "tool_use" {
			event.Delta = ""
			event.ToolCallDeltas = []interaction.ToolCallDelta{{Index: wire.Index, ID: wire.ContentBlock.ID, Type: "function", Name: wire.ContentBlock.Name}}
		}
	case "content_block_delta":
		if wire.Delta.Type == "input_json_delta" {
			event.Delta = ""
			event.ToolCallDeltas = []interaction.ToolCallDelta{{Index: wire.Index, Arguments: wire.Delta.PartialJSON}}
		}
	}
	if wire.Usage != nil {
		usage := toUnifiedUsage(anthropicUsage{InputTokens: wire.Usage.InputTokens, OutputTokens: wire.Usage.OutputTokens, CacheCreationInputTokens: wire.Usage.CacheCreationInputTokens, CacheReadInputTokens: wire.Usage.CacheReadInputTokens})
		event.Usage = &usage
	}
	return event, nil
}
