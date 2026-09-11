package openai

import (
	"strings"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

func TestDecodeResponsesMapsToChatPayload(t *testing.T) {
	request, err := DecodeResponsesBytes([]byte(`{"model":"gpt-x","instructions":"be brief","input":[{"role":"system","content":"base"},{"role":"user","content":[{"type":"text","text":"describe "},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}],"max_output_tokens":8,"temperature":0.2}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.Kind != interaction.RequestResponses || request.Model != "gpt-x" || request.Chat == nil || request.Responses == nil {
		t.Fatalf("request = %+v", request)
	}
	messages := request.Chat.Messages
	if len(messages) != 3 {
		t.Fatalf("messages = %+v", messages)
	}
	if messages[0].Role != "system" || messages[0].Content != "be brief" {
		t.Fatalf("instructions not leading system: %+v", messages[0])
	}
	if messages[1].Role != "system" || messages[1].Content != "base" {
		t.Fatalf("input system message = %+v", messages[1])
	}
	user := messages[2]
	if user.Role != "user" || user.Content != "describe " || len(user.Parts) != 1 || user.Parts[0].MediaType != "image/png" || user.Parts[0].DataURL == "" {
		t.Fatalf("user message = %+v", user)
	}
	if request.Chat.MaxOutputTokens == nil || *request.Chat.MaxOutputTokens != 8 {
		t.Fatalf("max_output_tokens not mapped: %+v", request.Chat.MaxOutputTokens)
	}
	if !request.HasImages() {
		t.Fatal("image-bearing responses input not detected")
	}
}

func TestDecodeResponsesRejectsInvalid(t *testing.T) {
	for _, body := range []string{
		`{"input":"hi"}`, // missing model
		`{"model":"m"}`,  // missing input
		`{"model":"m","input":[{"role":"bogus","content":"x"}]}`,
		`{"model":"m","input":[{"role":"user","content":123}]}`,
		`{"model":"m","input":[42]}`,
	} {
		if _, err := DecodeResponsesBytes([]byte(body)); err == nil {
			t.Fatalf("body accepted: %s", body)
		}
	}
}

func TestDecodeResponsesAcceptsInputTextAndInputImageAliases(t *testing.T) {
	request, err := DecodeResponsesBytes([]byte(`{"model":"m","input":[{"role":"user","content":[{"type":"input_text","text":"describe"},{"type":"input_image","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	user := request.Chat.Messages[0]
	if user.Content != "describe" || len(user.Parts) != 1 || user.Parts[0].MediaType != "image/png" {
		t.Fatalf("user = %+v", user)
	}
	if !request.HasImages() {
		t.Fatal("input_image not detected")
	}
}

func TestEncodeResponsesResponseShape(t *testing.T) {
	response := &interaction.UnifiedResponse{
		ID: "resp_1", Model: "gpt-x", CreatedAt: time.Unix(1700000000, 0),
		Usage: interaction.UnifiedUsage{InputTokens: 3, OutputTokens: 5},
		Choices: []interaction.Choice{{
			Message: interaction.Message{Role: "assistant", Content: "answer", ToolCalls: []interaction.ToolCall{{ID: "call_1", Type: "function", Function: interaction.ToolCallFunction{Name: "f", Arguments: `{"a":1}`}}}},
		}},
	}
	data, err := EncodeResponsesResponse(response, "gpt-x")
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, sample := range []string{`"object":"response"`, `"id":"resp_1"`, `"created_at":1700000000`, `"type":"output_text"`, `"answer"`, `"type":"function_call"`, `"name":"f"`, `"input_tokens":3`, `"output_tokens":5`, `"total_tokens":8`} {
		if !strings.Contains(body, sample) {
			t.Errorf("body missing %s:\n%s", sample, body)
		}
	}
}
