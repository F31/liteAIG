package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

func TestDecodeChatContentArrayWithInlineImage(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "what is this?"},
				{"type": "image_url", "image_url": {"url": "data:image/png;base64,iVBORw0KGgo="}}
			]}
		]
	}`)
	request, err := DecodeChatBytes(body)
	if err != nil {
		t.Fatal(err)
	}
	message := request.Chat.Messages[0]
	if message.Content != "what is this?" {
		t.Fatalf("content=%q", message.Content)
	}
	if len(message.Parts) != 1 {
		t.Fatalf("parts=%+v", message.Parts)
	}
	part := message.Parts[0]
	if part.Kind != interaction.ContentImage || part.MediaType != "image/png" || part.DataURL != "data:image/png;base64,iVBORw0KGgo=" || part.URL != "" {
		t.Fatalf("part=%+v", part)
	}
}

func TestDecodeChatContentArrayWithRemoteImage(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "caption this"},
				{"type": "image_url", "image_url": {"url": "https://example.com/pic.png", "detail": "high"}}
			]}
		]
	}`)
	request, err := DecodeChatBytes(body)
	if err != nil {
		t.Fatal(err)
	}
	message := request.Chat.Messages[0]
	if message.Content != "caption this" {
		t.Fatalf("content=%q", message.Content)
	}
	if len(message.Parts) != 1 {
		t.Fatalf("parts=%+v", message.Parts)
	}
	part := message.Parts[0]
	if part.Kind != interaction.ContentImage || part.URL != "https://example.com/pic.png" || part.DataURL != "" || part.MediaType != "" {
		t.Fatalf("part=%+v", part)
	}
}

func TestDecodeChatPlainStringKeepsEmptyParts(t *testing.T) {
	body := []byte(`{"model": "gpt-4o", "messages": [{"role": "user", "content": "hello"}]}`)
	request, err := DecodeChatBytes(body)
	if err != nil {
		t.Fatal(err)
	}
	message := request.Chat.Messages[0]
	if message.Content != "hello" || len(message.Parts) != 0 {
		t.Fatalf("message=%+v", message)
	}
}

func TestDecodeChatContentArrayConcatenatesTextParts(t *testing.T) {
	body := []byte(`{"model": "gpt-4o", "messages": [{"role": "user", "content": [
		{"type": "text", "text": "alpha"},
		{"type": "image_url", "image_url": {"url": "https://example.com/a.png"}},
		{"type": "text", "text": "beta"}
	]}]}`)
	request, err := DecodeChatBytes(body)
	if err != nil {
		t.Fatal(err)
	}
	message := request.Chat.Messages[0]
	if message.Content != "alphabeta" || len(message.Parts) != 1 {
		t.Fatalf("message.Content=%q parts=%+v", message.Content, message.Parts)
	}
}

func TestEncodeChatMultimodalEmitsContentArray(t *testing.T) {
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "gpt-4o", Chat: &interaction.ChatPayload{
		Messages: []interaction.Message{{
			Role:    "user",
			Content: "look at this",
			Parts: []interaction.ContentPart{
				{Kind: interaction.ContentImage, MediaType: "image/png", DataURL: "data:image/png;base64,iVBORw0KGgo="},
				{Kind: interaction.ContentImage, URL: "https://example.com/pic.png"},
			},
		}},
	}}
	encoded, err := EncodeChat(request)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	messages := wire["messages"].([]any)
	message := messages[0].(map[string]any)
	content, ok := message["content"].([]any)
	if !ok {
		t.Fatalf("multimodal content must be an array: %s", encoded)
	}
	if len(content) != 3 {
		t.Fatalf("content=%v", content)
	}
	text := content[0].(map[string]any)
	if text["type"] != "text" || text["text"] != "look at this" {
		t.Fatalf("leading part=%v", text)
	}
	first := content[1].(map[string]any)
	if first["type"] != "image_url" || first["image_url"].(map[string]any)["url"] != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("first image part=%v", first)
	}
	second := content[2].(map[string]any)
	if second["type"] != "image_url" || second["image_url"].(map[string]any)["url"] != "https://example.com/pic.png" {
		t.Fatalf("second image part=%v", second)
	}
}

func TestEncodeChatTextOnlyKeepsPlainStringContent(t *testing.T) {
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "gpt-4o", Chat: &interaction.ChatPayload{
		Messages: []interaction.Message{{Role: "user", Content: "hi"}},
	}}
	encoded, err := EncodeChat(request)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	message := wire["messages"].([]any)[0].(map[string]any)
	content, ok := message["content"].(string)
	if !ok || content != "hi" {
		t.Fatalf("text-only content must stay a plain string: %s", encoded)
	}
}

func TestEncodeChatDropsImagePartWithoutPayload(t *testing.T) {
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "gpt-4o", Chat: &interaction.ChatPayload{
		Messages: []interaction.Message{{
			Role:    "user",
			Content: "keep me",
			Parts:   []interaction.ContentPart{{Kind: interaction.ContentImage, MediaType: "image/png"}},
		}},
	}}
	encoded, err := EncodeChat(request)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	message := wire["messages"].([]any)[0].(map[string]any)
	content := message["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("payload-less image part must be dropped: %s", encoded)
	}
	text := content[0].(map[string]any)
	if text["type"] != "text" || text["text"] != "keep me" {
		t.Fatalf("content=%v", content)
	}
}

func TestDecodeChatResponseWithTextArrayContent(t *testing.T) {
	body := []byte(`{"id":"c1","model":"gpt-4o","created":1,"choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"text","text":"first"},{"type":"text","text":"second"}]},"finish_reason":"stop"}]}`)
	response, err := DecodeChatResponse(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if response.Choices[0].Message.Content != "firstsecond" || len(response.Choices[0].Message.Parts) != 0 {
		t.Fatalf("message=%+v", response.Choices[0].Message)
	}
}

func TestDecodeChatResponseIgnoresImageParts(t *testing.T) {
	body := []byte(`{"id":"c1","model":"gpt-4o","created":1,"choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"text","text":"ok"},{"type":"image_url","image_url":{"url":"https://example.com/o.png"}}]},"finish_reason":"stop"}]}`)
	response, err := DecodeChatResponse(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if response.Choices[0].Message.Content != "ok" {
		t.Fatalf("message=%+v", response.Choices[0].Message)
	}
}

func TestDecodeChatMalformedContentIsRejected(t *testing.T) {
	cases := []string{
		`{"model": "gpt-4o", "messages": [{"role": "user", "content": [{"text": "no type"}]}]}`,
		`{"model": "gpt-4o", "messages": [{"role": "user", "content": ["bare string"]}]}`,
		`{"model": "gpt-4o", "messages": [{"role": "user", "content": [{"type": "text"}]}]}`,
		`{"model": "gpt-4o", "messages": [{"role": "user", "content": [{"type": "image_url", "image_url": {"url": ""}}]}]}`,
		`{"model": "gpt-4o", "messages": [{"role": "user", "content": [{"type": "image_url", "image_url": 7}]}]}`,
		`{"model": "gpt-4o", "messages": [{"role": "user", "content": {"type": "text", "text": "not an array"}}]}`,
	}
	for _, body := range cases {
		_, err := DecodeChatBytes([]byte(body))
		if err == nil {
			t.Fatalf("malformed content must be rejected: %s", body)
		}
		var invalid *InvalidContentError
		if !errors.As(err, &invalid) {
			t.Fatalf("expected InvalidContentError, got %T: %v", err, err)
		}
	}
}
