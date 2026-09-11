package interaction

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestTextOnlyMessageJSONUnchanged(t *testing.T) {
	message := Message{Role: "user", Content: "hello", CacheControl: ""}
	before := []byte(`{"Role":"user","Content":"hello","ToolCallID":"","ToolCalls":null,"CacheControl":""}`)
	after, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("text-only Message JSON changed:\ngot  %s\nwant %s", after, before)
	}
}

func TestChatRequestJSONUnchangedWhenNoResponses(t *testing.T) {
	request := UnifiedRequest{Kind: RequestChat, Model: "m", Chat: &ChatPayload{Messages: []Message{{Role: "user", Content: "hi"}}}}
	before := []byte(`{"Kind":"chat","Model":"m","Stream":false,"Metadata":null,"SessionID":"","TaskID":"","RootTaskID":"","ParentTaskID":"","Chat":{"Messages":[{"Role":"user","Content":"hi","ToolCallID":"","ToolCalls":null,"CacheControl":""}],"MaxOutputTokens":null,"Temperature":null,"TopP":null,"Stop":null,"Tools":null,"ToolChoice":null,"ResponseFormat":null},"Embedding":null,"Tool":null,"Parameters":null}`)
	after, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("chat-only UnifiedRequest JSON changed:\ngot  %s\nwant %s", after, before)
	}
}

func TestMessageWithImagePartsMarshal(t *testing.T) {
	message := Message{Role: "user", Content: "what is this", Parts: []ContentPart{{Kind: ContentImage, MediaType: "image/png", DataURL: "data:image/png;base64,AAAA"}}}
	var raw map[string]json.RawMessage
	data, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["Parts"]; !ok {
		t.Fatalf("image-bearing message must serialize Parts: %s", data)
	}
	var round Message
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatal(err)
	}
	if !round.HasImages() || round.Content != "what is this" {
		t.Fatalf("round trip = %+v", round)
	}
}

func TestResponsesPayloadRoundTripAndExclusionFields(t *testing.T) {
	tokens := 64
	request := UnifiedRequest{
		Kind: RequestResponses, Model: "m", Stream: true,
		Responses: &ResponsesPayload{Messages: []Message{{Role: "user", Content: "hi"}}, MaxOutputTokens: &tokens},
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"Responses"`) {
		t.Fatalf("responses request must serialize Responses: %s", data)
	}
	var round UnifiedRequest
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatal(err)
	}
	if round.Kind != RequestResponses || round.Responses == nil || *round.Responses.MaxOutputTokens != 64 {
		t.Fatalf("round trip = %+v", round)
	}
}

func TestHasImagesScan(t *testing.T) {
	text := UnifiedRequest{Kind: RequestChat, Chat: &ChatPayload{Messages: []Message{{Role: "user", Content: "x"}}}}
	if text.HasImages() {
		t.Fatal("text-only request reports images")
	}
	withImage := text
	withImage.Chat.Messages[0].AddImage(ContentPart{MediaType: "image/jpeg", URL: "https://example.com/a.jpg"})
	if !withImage.HasImages() {
		t.Fatal("image request not detected")
	}
	responses := UnifiedRequest{Kind: RequestResponses, Chat: &ChatPayload{}}
	if responses.HasImages() {
		t.Fatal("empty responses request reports images")
	}
}
