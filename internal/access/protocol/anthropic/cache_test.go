package anthropic

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestCacheControlRoundTrip verifies an Anthropic request carrying an explicit
// cache_control marker on a text block and on the system prompt survives the
// decode→encode round trip (§11.3 prompt-cache pass-through).
func TestCacheControlRoundTrip(t *testing.T) {
	body := []byte(`{
		"model": "claude-3-5-sonnet",
		"max_tokens": 128,
		"system": [{"type": "text", "text": "be concise", "cache_control": {"type": "ephemeral"}}],
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "hello", "cache_control": {"type": "ephemeral"}}]}
		]
	}`)
	request, err := DecodeMessages(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	// System prompt carries the cache marker.
	if request.Chat.Messages[0].Role != "system" || request.Chat.Messages[0].CacheControl != "ephemeral" {
		t.Fatalf("system cache_control lost: %+v", request.Chat.Messages[0])
	}
	// User message carries the cache marker.
	if request.Chat.Messages[1].Role != "user" || request.Chat.Messages[1].CacheControl != "ephemeral" {
		t.Fatalf("user cache_control lost: %+v", request.Chat.Messages[1])
	}
	encoded, err := EncodeMessages(request)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		System []contentBlock `json:"system"`
		Msg    []struct {
			Role    string         `json:"role"`
			Content []contentBlock `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("decode re-encoded body: %v\n%s", err, encoded)
	}
	if len(wire.System) != 1 || wire.System[0].CacheControl == nil || wire.System[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("system cache_control not re-emitted: %+v", wire.System)
	}
	if len(wire.Msg) != 1 || len(wire.Msg[0].Content) != 1 || wire.Msg[0].Content[0].CacheControl == nil {
		t.Fatalf("user cache_control not re-emitted: %s", encoded)
	}
}

// A plain string system prompt without cache markers stays a string on encode
// (no behavioral change for existing clients).
func TestSystemPromptWithoutCacheStaysString(t *testing.T) {
	body := []byte(`{"model":"claude-3-5-haiku","max_tokens":16,"system":"be concise","messages":[{"role":"user","content":"hi"}]}`)
	request, err := DecodeMessages(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeMessages(request)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"system":"be concise"`)) {
		t.Fatalf("plain system prompt changed shape: %s", encoded)
	}
}

// A string system is decoded to a system message with an empty CacheControl so
// the round trip preserves the content without inventing a cache marker.
func TestSystemStringDecode(t *testing.T) {
	body := []byte(`{"model":"claude-3-5-haiku","max_tokens":16,"system":"be concise","messages":[{"role":"user","content":"hi"}]}`)
	request, err := DecodeMessages(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if request.Chat.Messages[0].CacheControl != "" {
		t.Fatalf("string system gained cache marker: %+v", request.Chat.Messages[0])
	}
}
