package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

// responsesWire is the minimal, stateless OpenAI /v1/responses request shape
// (spec §5.1 P0-Commercial). Input may be a plain string or an array of input
// items; each item may carry type/role/content. Function-call items are not
// modelled by the minimal surface and are ignored.
type responsesWire struct {
	Model           string             `json:"model"`
	Input           any                `json:"input"`
	Instructions    string             `json:"instructions,omitempty"`
	Stream          bool               `json:"stream,omitempty"`
	MaxOutputTokens *int               `json:"max_output_tokens,omitempty"`
	Temperature     *float64           `json:"temperature,omitempty"`
	TopP            *float64           `json:"top_p,omitempty"`
	Tools           []interaction.Tool `json:"tools,omitempty"`
	ToolChoice      json.RawMessage    `json:"tool_choice,omitempty"`
}

// DecodeResponsesBytes parses a stateless /v1/responses request into a
// canonical request. The gateway projects the input items onto ChatPayload so
// the existing seven-stage pipeline runs unchanged; the Responses kind keeps
// the request out of the default cache kinds.
func DecodeResponsesBytes(body []byte) (*interaction.UnifiedRequest, error) {
	var wire responsesWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}
	if wire.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	messages, err := responsesInput(wire.Input)
	if err != nil {
		return nil, err
	}
	if wire.Instructions != "" {
		messages = append([]interaction.Message{{Role: "system", Content: wire.Instructions}}, messages...)
	}
	request := &interaction.UnifiedRequest{
		Kind:      interaction.RequestResponses,
		Model:     wire.Model,
		Stream:    wire.Stream,
		Responses: &interaction.ResponsesPayload{Model: wire.Model, Messages: messages},
		Chat: &interaction.ChatPayload{
			Messages:        messages,
			MaxOutputTokens: wire.MaxOutputTokens,
			Temperature:     wire.Temperature,
			TopP:            wire.TopP,
			Tools:           wire.Tools,
			ToolChoice:      wire.ToolChoice,
		},
	}
	return request, nil
}

func responsesInput(input any) ([]interaction.Message, error) {
	if input == nil {
		return nil, fmt.Errorf("input is required")
	}
	if text, ok := input.(string); ok {
		if text == "" {
			return nil, fmt.Errorf("input must not be empty")
		}
		return []interaction.Message{{Role: "user", Content: text}}, nil
	}
	items, ok := input.([]any)
	if !ok {
		return nil, fmt.Errorf("input must be a string or an array of items")
	}
	var messages []interaction.Message
	for index, item := range items {
		message, err := responsesItem(item)
		if err != nil {
			return nil, fmt.Errorf("input item %d: %w", index, err)
		}
		if message != nil {
			messages = append(messages, *message)
		}
	}
	return messages, nil
}

func responsesItem(item any) (*interaction.Message, error) {
	object, ok := item.(map[string]any)
	if !ok {
		return nil, invalidContentf("input item must be an object")
	}
	itemType, _ := object["type"].(string)
	switch itemType {
	case "function_call", "function_call_output", "computer_call", "web_search_call", "reasoning":
		// Not modelled by the minimal stateless surface.
		return nil, nil
	}
	role, _ := object["role"].(string)
	switch role {
	case "":
		role = "user"
	case "developer":
		role = "system"
	case "system", "user", "assistant", "tool":
	default:
		return nil, invalidContentf("unsupported input role %q", role)
	}
	content, ok := object["content"]
	if !ok || content == nil {
		return nil, nil
	}
	text, parts, err := contentToParts(content)
	if err != nil {
		return nil, err
	}
	if text == "" && len(parts) == 0 {
		return nil, nil
	}
	return &interaction.Message{Role: role, Content: text, Parts: parts}, nil
}

// EncodeResponsesResponse renders a canonical response in the minimal
// stateless /v1/responses wire shape.
func EncodeResponsesResponse(response *interaction.UnifiedResponse, model string) ([]byte, error) {
	payload := responsesResponseWire{
		ID:        responseID(response),
		Object:    "response",
		CreatedAt: createdAt(response.CreatedAt),
		Status:    "completed",
		Model:     response.Model,
		Output:    []json.RawMessage{},
	}
	if model != "" {
		payload.Model = model
	}
	if len(response.Choices) == 0 {
		payload.Output = nil
	}
	for _, choice := range response.Choices {
		output := responseOutputWire{
			ID:      "msg_liteaig",
			Type:    "message",
			Role:    "assistant",
			Status:  "completed",
			Content: []json.RawMessage{},
		}
		if choice.Message.Content != "" {
			output.Content = append(output.Content, mustJSON(ResponseTextPart{Type: "output_text", Text: choice.Message.Content, Annotations: []any{}}))
		}
		for _, call := range choice.Message.ToolCalls {
			output.Content = append(output.Content, mustJSON(FunctionCallOutput{Type: "function_call", ID: call.ID, CallID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments}))
		}
		payload.Output = append(payload.Output, mustJSON(output))
	}
	if usage := response.Usage; usage.TotalTokens() > 0 || usage.CacheReadTokens > 0 || usage.InputTokens > 0 || usage.OutputTokens > 0 {
		payload.Usage = &usageWire{
			InputTokens:  usage.InputTokens,
			OutputTokens: usage.OutputTokens,
			TotalTokens:  usage.TotalTokens(),
		}
	}
	return json.Marshal(payload)
}

func DecodeResponses(reader io.Reader) (*interaction.UnifiedRequest, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return DecodeResponsesBytes(body)
}

type responsesResponseWire struct {
	ID        string            `json:"id"`
	Object    string            `json:"object"`
	CreatedAt int64             `json:"created_at"`
	Status    string            `json:"status"`
	Model     string            `json:"model"`
	Output    []json.RawMessage `json:"output"`
	Usage     *usageWire        `json:"usage,omitempty"`
}

type usageWire struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

type responseOutputWire struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Role    string            `json:"role"`
	Status  string            `json:"status"`
	Content []json.RawMessage `json:"content"`
}

// ResponseTextPart is the text content block of a responses message output.
type ResponseTextPart struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []any  `json:"annotations"`
}

// FunctionCallOutput is the function-call content block of a responses output.
type FunctionCallOutput struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func responseID(response *interaction.UnifiedResponse) string {
	if response != nil && response.ID != "" {
		return response.ID
	}
	return "resp_liteaig"
}

func createdAt(at time.Time) int64 {
	if at.IsZero() {
		at = time.Now()
	}
	return at.Unix()
}

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return data
}
