package liteaig

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientCallsGatewaySurfaces(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get(apiVersionHeader) != DefaultAPIVersion {
			t.Fatalf("api version = %q", r.Header.Get(apiVersionHeader))
		}
		seen = append(seen, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]string{{"id": "default-chat"}}})
		case "/v1/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "chat", "model": "default-chat", "choices": []map[string]any{{"index": 0, "message": map[string]string{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
		case "/v1/responses":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "resp", "status": "completed", "model": "default-chat", "output": []map[string]any{{"type": "message", "role": "assistant", "content": []map[string]string{{"type": "output_text", "text": "hello"}}}}, "usage": map[string]int{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}})
		case "/v1/embeddings":
			_ = json.NewEncoder(w).Encode(map[string]any{"model": "default-chat", "data": []map[string]any{{"index": 0, "embedding": []float64{0.1}}}, "usage": map[string]int{"prompt_tokens": 1, "total_tokens": 1}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, "key")
	if err != nil {
		t.Fatal(err)
	}
	models, err := client.ListModels(context.Background())
	if err != nil || len(models.Data) != 1 || models.Data[0].ID != "default-chat" {
		t.Fatalf("models=%+v err=%v", models, err)
	}
	chat, err := client.CreateChatCompletion(context.Background(), ChatCompletionRequest{Model: "default-chat", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil || chat.Choices[0].Message.Content != "ok" {
		t.Fatalf("chat=%+v err=%v", chat, err)
	}
	response, err := client.CreateResponse(context.Background(), ResponsesRequest{Model: "default-chat", Input: "hi"})
	if err != nil || response.OutputText() != "hello" {
		t.Fatalf("response=%+v text=%q err=%v", response, response.OutputText(), err)
	}
	embedding, err := client.CreateEmbedding(context.Background(), EmbeddingRequest{Model: "default-chat", Input: []string{"hi"}})
	if err != nil || len(embedding.Data) != 1 || len(embedding.Data[0].Embedding) != 1 {
		t.Fatalf("embedding=%+v err=%v", embedding, err)
	}

	want := []string{"GET /v1/models", "POST /v1/chat/completions", "POST /v1/responses", "POST /v1/embeddings"}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Fatalf("seen=%v want=%v", seen, want)
	}
}

func TestClientChatCompletionStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer key" || r.Header.Get(apiVersionHeader) != DefaultAPIVersion {
			t.Fatalf("headers=%v", r.Header)
		}
		var body ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !body.Stream || body.Model != "default-chat" {
			t.Fatalf("body=%+v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chat\",\"model\":\"default-chat\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hel\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chat\",\"model\":\"default-chat\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	client, err := New(server.URL, "key")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.CreateChatCompletionStream(context.Background(), ChatCompletionRequest{Model: "default-chat", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	first, err := stream.Recv()
	if err != nil || first.Choices[0].Delta.Content != "hel" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := stream.Recv()
	if err != nil || second.Choices[0].Delta.Content != "lo" || second.Choices[0].FinishReason != "stop" || second.Usage.TotalTokens != 2 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if event, err := stream.Recv(); err != io.EOF || event != nil {
		t.Fatalf("final event=%+v err=%v", event, err)
	}
}

func TestClientMCPHelpers(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" || r.Method != http.MethodPost {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer key" || r.Header.Get(apiVersionHeader) != DefaultAPIVersion {
			t.Fatalf("headers=%v", r.Header)
		}
		var body struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      any             `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		seen = append(seen, body.Method)
		if body.JSONRPC != "2.0" {
			t.Fatalf("jsonrpc=%q", body.JSONRPC)
		}
		w.Header().Set("Content-Type", "application/json")
		switch body.Method {
		case "server/discover":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": body.ID, "result": map[string]any{"protocolVersion": "2026-07-28"}})
		case "tools/call":
			var params MCPToolCallParams
			if err := json.Unmarshal(body.Params, &params); err != nil {
				t.Fatal(err)
			}
			if params.Name != "invoice.read" {
				t.Fatalf("params=%+v", params)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": body.ID, "result": map[string]any{"content": "ok"}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": body.ID, "error": map[string]any{"code": -32601, "message": "method not found"}})
		}
	}))
	defer server.Close()
	client, err := New(server.URL, "key")
	if err != nil {
		t.Fatal(err)
	}
	discover, err := client.CallMCP(context.Background(), MCPRequest{ID: "d1", Method: "server/discover", Params: map[string]any{}})
	if err != nil || !strings.Contains(string(discover.Result), "2026-07-28") {
		t.Fatalf("discover=%+v err=%v", discover, err)
	}
	call, err := client.CallMCPTool(context.Background(), "t1", "invoice.read", map[string]string{"id": "42"})
	if err != nil || string(call.Result) != `{"content":"ok"}` {
		t.Fatalf("call=%+v err=%v", call, err)
	}
	missing, err := client.CallMCP(context.Background(), MCPRequest{ID: "x", Method: "unknown"})
	if err != nil || missing.Error == nil || missing.Error.Code != -32601 {
		t.Fatalf("missing=%+v err=%v", missing, err)
	}
	if strings.Join(seen, ",") != "server/discover,tools/call,unknown" {
		t.Fatalf("seen=%v", seen)
	}
}

func TestClientA2AHelpers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a2a" || r.Method != http.MethodPost {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer key" || r.Header.Get(apiVersionHeader) != DefaultAPIVersion {
			t.Fatalf("headers=%v", r.Header)
		}
		var body struct {
			JSONRPC string `json:"jsonrpc"`
			ID      any    `json:"id"`
			Method  string `json:"method"`
			Params  struct {
				Message A2AMessage `json:"message"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if body.JSONRPC != "2.0" || body.Method != "SendMessage" || body.Params.Message.MessageID != "m1" || body.Params.Message.Role != "ROLE_USER" || body.Params.Message.Parts[0].Text != "hello" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": body.ID, "error": map[string]any{"code": -32601, "message": "method not found"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": body.ID, "result": map[string]any{"message": map[string]any{"messageId": "m1", "role": "ROLE_AGENT", "parts": []map[string]string{{"text": "reply"}}}}})
	}))
	defer server.Close()
	client, err := New(server.URL, "key")
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.SendA2AMessage(context.Background(), "1", A2AMessage{MessageID: "m1", Role: "ROLE_USER", Parts: []A2APart{{Text: "hello"}}})
	if err != nil || !strings.Contains(string(response.Result), "ROLE_AGENT") {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	missing, err := client.SendA2AMessage(context.Background(), "2", A2AMessage{MessageID: "bad", Role: "ROLE_USER", Parts: []A2APart{{Text: "hello"}}})
	if err != nil || missing.Error == nil || missing.Error.Code != -32601 {
		t.Fatalf("missing=%+v err=%v", missing, err)
	}
}

func TestClientA2AStreamHelper(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a2a" || r.Method != http.MethodPost {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer key" || r.Header.Get(apiVersionHeader) != DefaultAPIVersion {
			t.Fatalf("headers=%v", r.Header)
		}
		var body struct {
			JSONRPC string `json:"jsonrpc"`
			ID      any    `json:"id"`
			Method  string `json:"method"`
			Params  struct {
				Message A2AMessage `json:"message"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.JSONRPC != "2.0" || body.Method != "SendStreamingMessage" || body.Params.Message.MessageID != "m1" || body.Params.Message.Parts[0].Text != "hello" {
			t.Fatalf("body=%+v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message\",\"message\":{\"parts\":[{\"kind\":\"text\",\"text\":\"hel\"}]}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message\",\"message\":{\"parts\":[{\"kind\":\"text\",\"text\":\"lo\"}]}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"completed\"}\n\n"))
	}))
	defer server.Close()
	client, err := New(server.URL, "key")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.SendA2AMessageStream(context.Background(), "1", A2AMessage{MessageID: "m1", Role: "ROLE_USER", Parts: []A2APart{{Text: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	first, err := stream.Recv()
	if err != nil || first.Delta != "hel" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := stream.Recv()
	if err != nil || second.Delta != "lo" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if event, err := stream.Recv(); err != io.EOF || event != nil {
		t.Fatalf("final event=%+v err=%v", event, err)
	}
}

func TestClientA2AStreamSurfacesJSONRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":\"1\",\"error\":{\"code\":-32000,\"message\":\"remote boom\"}}\n\n"))
	}))
	defer server.Close()
	client, err := New(server.URL, "key")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.SendA2AMessageStream(context.Background(), "1", A2AMessage{MessageID: "m1", Role: "ROLE_USER", Parts: []A2APart{{Text: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if event, err := stream.Recv(); err == nil || event != nil || !strings.Contains(err.Error(), "remote boom") {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}

func TestClientVersionOverrideAndErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(apiVersionHeader) != "2026-09-01" {
			t.Fatalf("api version = %q", r.Header.Get(apiVersionHeader))
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"UNSUPPORTED_API_VERSION"}}`))
	}))
	defer server.Close()
	client, err := New(server.URL+"/", "key", WithAPIVersion("2026-09-01"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListModels(context.Background())
	if err == nil || !strings.Contains(err.Error(), "UNSUPPORTED_API_VERSION") {
		t.Fatalf("err=%v", err)
	}
	if _, err := New("", "key"); err == nil {
		t.Fatal("missing base URL accepted")
	}
}
