package openai

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

// contentValue renders canonical message content for the OpenAI wire. Text-only
// messages keep Content as a plain string so existing egress bodies are
// byte-identical; multimodal messages (non-empty Parts) become an ordered array
// with one leading text block from Content (when non-empty) followed by one
// image_url object per image part. An image part with neither an inline DataURL
// nor a remote URL is dropped (never produced by a valid decode); if nothing is
// emitted the message falls back to its plain text content.
func contentValue(content string, parts []interaction.ContentPart) any {
	if len(parts) == 0 {
		return content
	}
	entries := make([]any, 0, len(parts)+1)
	if content != "" {
		entries = append(entries, map[string]any{"type": "text", "text": content})
	}
	for _, part := range parts {
		if part.Kind != interaction.ContentImage {
			continue
		}
		location := part.DataURL
		if location == "" {
			location = part.URL
		}
		if location == "" {
			continue
		}
		entries = append(entries, map[string]any{"type": "image_url", "image_url": map[string]any{"url": location}})
	}
	if len(entries) == 0 {
		return content
	}
	return entries
}

type chatChoiceWire struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}
type chatResponseWire struct {
	ID      string           `json:"id"`
	Model   string           `json:"model"`
	Created int64            `json:"created"`
	Choices []chatChoiceWire `json:"choices"`
	Usage   Usage            `json:"usage"`
}

// EncodeChatResponse renders a canonical chat response in the OpenAI
// wire shape. model overrides the reported model name (the gateway echoes the
// requested logical model, not the upstream model).
func EncodeChatResponse(response *interaction.UnifiedResponse, model string) ([]byte, error) {
	if response == nil {
		return nil, fmt.Errorf("chat response is required")
	}
	wire := chatResponseWire{
		ID:      valueOr(response.ID, "chatcmpl-liteaig"),
		Model:   model,
		Created: time.Now().Unix(),
		Usage:   Usage{PromptTokens: response.Usage.InputTokens, CompletionTokens: response.Usage.OutputTokens, TotalTokens: response.Usage.TotalTokens()},
	}
	for _, choice := range response.Choices {
		finish := response.StopReason
		if finish == "" {
			finish = "stop"
		}
		wire.Choices = append(wire.Choices, chatChoiceWire{Index: choice.Index, Message: Message{Role: choice.Message.Role, Content: contentValue(choice.Message.Content, choice.Message.Parts), ToolCalls: choice.Message.ToolCalls}, FinishReason: finish})
	}
	if wire.Choices == nil {
		wire.Choices = []chatChoiceWire{}
	}
	return json.Marshal(wire)
}

type embeddingDataWire struct {
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}
type embeddingResponseWire struct {
	Model string              `json:"model"`
	Data  []embeddingDataWire `json:"data"`
	Usage Usage               `json:"usage"`
}

// EncodeEmbeddingResponse renders a canonical embedding response.
func EncodeEmbeddingResponse(response *interaction.UnifiedResponse, model string) ([]byte, error) {
	if response == nil {
		return nil, fmt.Errorf("embedding response is required")
	}
	wire := embeddingResponseWire{
		Model: model,
		Usage: Usage{PromptTokens: response.Usage.InputTokens, CompletionTokens: response.Usage.OutputTokens, TotalTokens: response.Usage.TotalTokens()},
	}
	for i, vector := range response.Embeddings {
		wire.Data = append(wire.Data, embeddingDataWire{Index: i, Embedding: vector})
	}
	return json.Marshal(wire)
}

type streamChunkToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}
type streamChunkChoice struct {
	Index int `json:"index"`
	Delta struct {
		Role      string                `json:"role,omitempty"`
		Content   string                `json:"content,omitempty"`
		ToolCalls []streamChunkToolCall `json:"tool_calls,omitempty"`
	} `json:"delta"`
	FinishReason string `json:"finish_reason"`
}
type streamChunkWire struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Model   string              `json:"model"`
	Choices []streamChunkChoice `json:"choices"`
	Usage   *Usage              `json:"usage,omitempty"`
}

// EncodeStreamChunk renders one normalized stream event as an OpenAI
// chat.completion.chunk payload.
func EncodeStreamChunk(event interaction.StreamEvent, model string) ([]byte, error) {
	wire := streamChunkWire{
		ID:     valueOr(event.ID, "chatcmpl-liteaig"),
		Object: "chat.completion.chunk",
		Model:  model,
	}
	choice := streamChunkChoice{Index: event.Index, FinishReason: event.StopReason}
	choice.Delta.Content = event.Delta
	for _, item := range event.ToolCallDeltas {
		call := streamChunkToolCall{Index: item.Index, ID: item.ID, Type: item.Type}
		call.Function.Name = item.Name
		call.Function.Arguments = item.Arguments
		choice.Delta.ToolCalls = append(choice.Delta.ToolCalls, call)
	}
	wire.Choices = []streamChunkChoice{choice}
	if event.Usage != nil {
		wire.Usage = &Usage{PromptTokens: event.Usage.InputTokens, CompletionTokens: event.Usage.OutputTokens, TotalTokens: event.Usage.TotalTokens()}
	}
	return json.Marshal(wire)
}

// StreamDone is the OpenAI terminal stream marker payload.
func StreamDone() []byte {
	return []byte("[DONE]")
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
