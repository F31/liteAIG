package openai

import (
	"bytes"
	"os"
	"testing"
)

func TestChatFixtureRoundTrip(t *testing.T) {
	data, err := os.ReadFile("testdata/chat.json")
	if err != nil {
		t.Fatal(err)
	}
	request, err := DecodeChat(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if request.Model != "default-chat" || request.Chat.Messages[0].Content != "hello" || string(request.Parameters["seed"]) != "42" {
		t.Fatalf("request=%+v", request)
	}
	encoded, err := EncodeChat(request)
	if err != nil || !bytes.Contains(encoded, []byte(`"seed":42`)) {
		t.Fatalf("EncodeChat()=%s,%v", encoded, err)
	}
}
func TestNormalizeStream(t *testing.T) {
	event, err := NormalizeStream([]byte(`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`))
	if err != nil || event.Delta != "hi" || !event.Final {
		t.Fatalf("NormalizeStream()=%+v,%v", event, err)
	}
}

// TestCachedTokensMapToUnifiedUsage verifies prompt_tokens_details.cached_tokens
// lands in both CacheReadTokens and CachedInputTokens so accounting and routing
// can price and score cache hits (§11.3, Stage 13).
func TestCachedTokensMapToUnifiedUsage(t *testing.T) {
	chat := []byte(`{
		"id": "chat1", "model": "gpt-4o-mini",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "hi"}, "finish_reason": "stop"}],
		"usage": {
			"prompt_tokens": 100, "completion_tokens": 5, "total_tokens": 105,
			"prompt_tokens_details": {"cached_tokens": 90},
			"completion_tokens_details": {"reasoning_tokens": 0}
		}
	}`)
	response, err := DecodeChatResponse(bytes.NewReader(chat))
	if err != nil {
		t.Fatal(err)
	}
	if response.Usage.CacheReadTokens != 90 || response.Usage.CachedInputTokens != 90 {
		t.Fatalf("cached_tokens not mapped: %+v", response.Usage)
	}
	if response.Usage.InputTokens != 100 || response.Usage.OutputTokens != 5 {
		t.Fatalf("input/output tokens lost: %+v", response.Usage)
	}
}

func TestEmbeddingNormalization(t *testing.T) {
	request, err := DecodeEmbedding(bytes.NewReader([]byte(`{"model":"embed","input":["one","two"]}`)))
	if err != nil {
		t.Fatal(err)
	}
	if request.Kind != "embedding" || len(request.Embedding.Inputs) != 2 {
		t.Fatalf("request=%+v", request)
	}
	response, err := DecodeEmbeddingResponse(bytes.NewReader([]byte(`{"model":"embed","data":[{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":2,"total_tokens":2}}`)))
	if err != nil || len(response.Embeddings) != 1 || response.Usage.InputTokens != 2 {
		t.Fatalf("response=%+v error=%v", response, err)
	}
}
