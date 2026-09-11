package anthropic

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

// TestDecodeInboundBase64Image verifies a user content array carrying a text
// block followed by an inline base64 image block decodes to a canonical
// message whose text lives in Content and whose image survives as one ordered
// ContentPart with the media type and data URL preserved.
func TestDecodeInboundBase64Image(t *testing.T) {
	body := []byte(`{
		"model": "claude-3-5-haiku",
		"max_tokens": 64,
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "what is in this image?"},
				{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "iVBORw0KGgo="}}
			]}
		]
	}`)
	request, err := DecodeMessages(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	msg := request.Chat.Messages[0]
	if msg.Role != "user" || msg.Content != "what is in this image?" {
		t.Fatalf("text lost: %+v", msg)
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("parts=%+v", msg.Parts)
	}
	part := msg.Parts[0]
	if part.Kind != interaction.ContentImage || part.MediaType != "image/png" || part.DataURL != "data:image/png;base64,iVBORw0KGgo=" || part.URL != "" {
		t.Fatalf("part=%+v", part)
	}
}

// TestDecodeInboundURLImage verifies a url-source image block decodes to a
// ContentPart with only the remote URL set.
func TestDecodeInboundURLImage(t *testing.T) {
	body := []byte(`{
		"model": "claude-3-5-haiku",
		"max_tokens": 64,
		"messages": [
			{"role": "user", "content": [
				{"type": "image", "source": {"type": "url", "url": "https://example.com/cat.png"}}
			]}
		]
	}`)
	request, err := DecodeMessages(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	msg := request.Chat.Messages[0]
	if msg.Content != "" || len(msg.Parts) != 1 {
		t.Fatalf("msg=%+v", msg)
	}
	part := msg.Parts[0]
	if part.Kind != interaction.ContentImage || part.URL != "https://example.com/cat.png" || part.DataURL != "" || part.MediaType != "" {
		t.Fatalf("part=%+v", part)
	}
}

// TestDecodePlainStringContent verifies a plain-string message is untouched by
// the image handling.
func TestDecodePlainStringContent(t *testing.T) {
	body := []byte(`{"model":"claude-3-5-haiku","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`)
	request, err := DecodeMessages(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Chat.Messages) != 1 || request.Chat.Messages[0].Content != "hi" || len(request.Chat.Messages[0].Parts) != 0 {
		t.Fatalf("msg=%+v", request.Chat.Messages[0])
	}
}

// TestEncodeMultimodalMessage verifies a canonical multimodal message re-encodes
// as a text block followed by its image blocks in order, and that the wire body
// round-trips back to the same canonical message.
func TestEncodeMultimodalMessage(t *testing.T) {
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "claude-3-5-sonnet", Chat: &interaction.ChatPayload{
		MaxOutputTokens: intPtr(64),
		Messages: []interaction.Message{{
			Role:    "user",
			Content: "what is in this image?",
			Parts: []interaction.ContentPart{
				{Kind: interaction.ContentImage, MediaType: "image/png", DataURL: "data:image/png;base64,iVBORw0KGgo="},
				{Kind: interaction.ContentImage, MediaType: "image/jpeg", DataURL: "data:image/jpeg;base64,/9j/2Q=="},
			},
		}},
	}}
	encoded, err := EncodeMessages(request)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Messages []struct {
			Role    string         `json:"role"`
			Content []contentBlock `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("decode re-encoded body: %v\n%s", err, encoded)
	}
	if len(wire.Messages) != 1 {
		t.Fatalf("messages=%+v", wire.Messages)
	}
	blocks := wire.Messages[0].Content
	if len(blocks) != 3 {
		t.Fatalf("blocks=%+v", blocks)
	}
	if blocks[0].Type != "text" || blocks[0].Text != "what is in this image?" {
		t.Fatalf("text block=%+v", blocks[0])
	}
	checkImage := func(block contentBlock, mediaType, data string) {
		t.Helper()
		if block.Type != "image" || block.Source == nil || block.Source.Type != "base64" || block.Source.MediaType != mediaType || block.Source.Data != data {
			t.Fatalf("image block=%+v", block)
		}
	}
	checkImage(blocks[1], "image/png", "iVBORw0KGgo=")
	checkImage(blocks[2], "image/jpeg", "/9j/2Q==")

	decoded, err := DecodeMessagesBytes(encoded)
	if err != nil {
		t.Fatal(err)
	}
	msg := decoded.Chat.Messages[0]
	if msg.Content != "what is in this image?" || len(msg.Parts) != 2 || msg.Parts[0].DataURL != "data:image/png;base64,iVBORw0KGgo=" || msg.Parts[1].DataURL != "data:image/jpeg;base64,/9j/2Q==" {
		t.Fatalf("round trip=%+v parts=%+v", msg, msg.Parts)
	}
}

// TestEncodeTextOnlyByteIdentical verifies a text-only canonical message still
// produces a plain-string wire body identical to the pre-image layout (no
// content-block array, no parts).
func TestEncodeTextOnlyByteIdentical(t *testing.T) {
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "claude-3-5-haiku", Chat: &interaction.ChatPayload{
		MaxOutputTokens: intPtr(64),
		Messages:        []interaction.Message{{Role: "user", Content: "hi"}},
	}}
	encoded, err := EncodeMessages(request)
	if err != nil {
		t.Fatal(err)
	}
	expected := []byte(`{"max_tokens":64,"messages":[{"role":"user","content":"hi"}],"model":"claude-3-5-haiku","stream":false}`)
	if !bytes.Equal(encoded, expected) {
		t.Fatalf("text-only body changed:\n got %s\nwant %s", encoded, expected)
	}
}

// TestEncodeRemoteURLImageErrors verifies an image part carrying only a remote
// URL is rejected on encode with a clear error instead of being silently
// dropped.
func TestEncodeRemoteURLImageErrors(t *testing.T) {
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "claude-3-5-sonnet", Chat: &interaction.ChatPayload{
		MaxOutputTokens: intPtr(64),
		Messages: []interaction.Message{{
			Role:    "user",
			Content: "what is this?",
			Parts:   []interaction.ContentPart{{Kind: interaction.ContentImage, URL: "https://example.com/cat.png"}},
		}},
	}}
	encoded, err := EncodeMessages(request)
	if err == nil {
		t.Fatalf("expected error for remote-only image, got %s", encoded)
	}
	if !strings.Contains(err.Error(), "anthropic upstream requires inline data-URL images") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCacheControlCoexistsWithImage verifies a text block's cache_control
// marker survives next to a following image block on encode and through the
// round trip.
func TestCacheControlCoexistsWithImage(t *testing.T) {
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "claude-3-5-sonnet", Chat: &interaction.ChatPayload{
		MaxOutputTokens: intPtr(128),
		Messages: []interaction.Message{{
			Role:         "user",
			Content:      "remember this",
			CacheControl: "ephemeral",
			Parts:        []interaction.ContentPart{{Kind: interaction.ContentImage, MediaType: "image/png", DataURL: "data:image/png;base64,iVBORw0KGgo="}},
		}},
	}}
	encoded, err := EncodeMessages(request)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Messages []struct {
			Content []contentBlock `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("decode re-encoded body: %v\n%s", err, encoded)
	}
	blocks := wire.Messages[0].Content
	if len(blocks) != 2 {
		t.Fatalf("blocks=%+v", blocks)
	}
	if blocks[0].Type != "text" || blocks[0].CacheControl == nil || blocks[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("cache marker lost on text block: %+v", blocks[0])
	}
	if blocks[1].Type != "image" || blocks[1].Source == nil || blocks[1].Source.Data != "iVBORw0KGgo=" {
		t.Fatalf("image block=%+v", blocks[1])
	}
	decoded, err := DecodeMessagesBytes(encoded)
	if err != nil {
		t.Fatal(err)
	}
	msg := decoded.Chat.Messages[0]
	if msg.Content != "remember this" || msg.CacheControl != "ephemeral" || len(msg.Parts) != 1 || msg.Parts[0].DataURL != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("round trip=%+v", msg)
	}
}
