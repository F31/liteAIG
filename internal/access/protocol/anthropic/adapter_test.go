package anthropic

import (
	"bytes"
	"os"
	"testing"
)

func TestMessagesFixtureRoundTrip(t *testing.T) {
	data, err := os.ReadFile("testdata/messages.json")
	if err != nil {
		t.Fatal(err)
	}
	request, err := DecodeMessages(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Chat.Messages) != 2 || request.Chat.Messages[0].Role != "system" || string(request.Parameters["top_k"]) != "4" {
		t.Fatalf("request=%+v", request)
	}
	encoded, err := EncodeMessages(request)
	if err != nil || !bytes.Contains(encoded, []byte(`"system":"be concise"`)) {
		t.Fatalf("EncodeMessages()=%s,%v", encoded, err)
	}
}
func TestNormalizeStream(t *testing.T) {
	event, err := NormalizeStream([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`))
	if err != nil || event.Delta != "hi" {
		t.Fatalf("NormalizeStream()=%+v,%v", event, err)
	}
}
