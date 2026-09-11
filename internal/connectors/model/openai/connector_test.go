package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	platformsecrets "github.com/F31/liteAIG/internal/platform/secrets"
)

type secrets map[string][]byte

func (s secrets) Resolve(_ context.Context, ref string) ([]byte, error) {
	value, ok := s[ref]
	if !ok {
		return nil, errors.New("missing")
	}
	return value, nil
}

type writer struct{ events []interaction.StreamEvent }

func (w *writer) WriteChunk(_ context.Context, chunk contracts.StreamChunk) error {
	w.events = append(w.events, chunk.Event)
	return nil
}
func chatRequest() *interaction.UnifiedRequest {
	max := 16
	return &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "model", Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "hi"}}, MaxOutputTokens: &max}}
}
func connectorFor(t *testing.T, server *httptest.Server, timeout time.Duration) *Connector {
	t.Helper()
	config := DefaultConfig()
	config.BaseURL = server.URL
	config.SecretRef = "secret://provider"
	config.RequestTimeout = timeout
	config.AllowInsecureHTTP = true
	connector, err := New(config, server.Client(), secrets{"secret://provider": []byte("provider-secret")})
	if err != nil {
		t.Fatal(err)
	}
	return connector
}

func TestInvokeNormalAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer provider-secret" {
			t.Error("missing server-side credential")
		}
		response.Header().Set("Content-Type", "application/json")
		response.Write([]byte(`{"id":"r1","model":"model","created":1,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
	}))
	defer server.Close()
	result, err := connectorFor(t, server, time.Second).Invoke(context.Background(), contracts.InvocationRequest{Request: chatRequest()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Response.Usage.TotalTokens() != 5 || result.Response.Choices[0].Message.Content != "ok" {
		t.Fatalf("response=%+v", result.Response)
	}
}

func TestInvokeRerank(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/rerank" {
			t.Fatalf("path=%s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer provider-secret" {
			t.Fatal("missing server-side credential")
		}
		response.Header().Set("Content-Type", "application/json")
		response.Write([]byte(`{"model":"rerank-model","results":[{"index":1,"relevance_score":0.8,"document":"Berlin"}],"usage":{"prompt_tokens":4,"total_tokens":4}}`))
	}))
	defer server.Close()
	topN := 1
	request := &interaction.UnifiedRequest{Kind: interaction.RequestRerank, Model: "rerank-model", Rerank: &interaction.RerankPayload{Query: "capital", Documents: []string{"Paris", "Berlin"}, TopN: &topN}}
	result, err := connectorFor(t, server, time.Second).Invoke(context.Background(), contracts.InvocationRequest{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Response.Rerank) != 1 || result.Response.Rerank[0].Index != 1 || result.Response.Rerank[0].RelevanceScore != 0.8 || result.Response.Usage.TotalTokens() != 4 {
		t.Fatalf("response=%+v", result.Response)
	}
}

func TestInvokeAudioTranscription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/audio/transcriptions" {
			t.Fatalf("path=%s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer provider-secret" || !strings.HasPrefix(request.Header.Get("Content-Type"), "multipart/form-data") {
			t.Fatalf("headers=%v", request.Header)
		}
		if err := request.ParseMultipartForm(1024); err != nil {
			t.Fatal(err)
		}
		if request.FormValue("model") != "whisper-1" || request.FormValue("language") != "en" {
			t.Fatalf("form=%v", request.Form)
		}
		file, _, err := request.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if string(data) != "audio-bytes" {
			t.Fatalf("file=%q", data)
		}
		response.Header().Set("Content-Type", "application/json")
		response.Write([]byte(`{"text":"hello"}`))
	}))
	defer server.Close()
	temp := 0.1
	request := &interaction.UnifiedRequest{Kind: interaction.RequestAudio, Model: "whisper-1", Audio: &interaction.AudioPayload{Filename: "sample.wav", MediaType: "audio/wav", Data: []byte("audio-bytes"), Language: "en", Temperature: &temp}}
	result, err := connectorFor(t, server, time.Second).Invoke(context.Background(), contracts.InvocationRequest{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if result.Response.Audio.Text != "hello" || result.Response.Choices[0].Message.Content != "hello" {
		t.Fatalf("response=%+v", result.Response)
	}
}

func TestInvokeBatchCreate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/batches" {
			t.Fatalf("path=%s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer provider-secret" {
			t.Fatal("missing server-side credential")
		}
		body, _ := io.ReadAll(request.Body)
		if strings.Contains(string(body), `"model"`) || !strings.Contains(string(body), `"input_file_id":"file_1"`) {
			t.Fatalf("body=%s", body)
		}
		response.Header().Set("Content-Type", "application/json")
		response.Write([]byte(`{"id":"batch_1","object":"batch","endpoint":"/v1/chat/completions","input_file_id":"file_1","status":"validating","completion_window":"24h"}`))
	}))
	defer server.Close()
	request := &interaction.UnifiedRequest{Kind: interaction.RequestBatch, Model: "batch-logical", Batch: &interaction.BatchPayload{InputFileID: "file_1", Endpoint: "/v1/chat/completions", CompletionWindow: "24h"}}
	result, err := connectorFor(t, server, time.Second).Invoke(context.Background(), contracts.InvocationRequest{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if result.Response.Batch.ID != "batch_1" || result.Response.Batch.Status != "validating" || len(result.Response.Batch.Raw) == 0 {
		t.Fatalf("response=%+v", result.Response.Batch)
	}
}

func TestInvokeBatchLifecycle(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		seen = append(seen, request.Method+" "+request.URL.RequestURI())
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/v1/batches" {
			response.Write([]byte(`{"object":"list","data":[{"id":"batch_1","object":"batch","status":"completed"}],"has_more":false}`))
			return
		}
		response.Write([]byte(`{"id":"batch_1","object":"batch","status":"in_progress"}`))
	}))
	defer server.Close()
	connector := connectorFor(t, server, time.Second)
	for _, request := range []*interaction.UnifiedRequest{
		{Kind: interaction.RequestBatch, Model: "batch-logical", Batch: &interaction.BatchPayload{Operation: "retrieve", ID: "batch_1"}},
		{Kind: interaction.RequestBatch, Model: "batch-logical", Batch: &interaction.BatchPayload{Operation: "cancel", ID: "batch_1"}},
		{Kind: interaction.RequestBatch, Model: "batch-logical", Batch: &interaction.BatchPayload{Operation: "list", Limit: 10, After: "batch_0"}},
	} {
		result, err := connector.Invoke(context.Background(), contracts.InvocationRequest{Request: request})
		if err != nil {
			t.Fatal(err)
		}
		if result.Response.Batch == nil {
			t.Fatalf("response=%+v", result.Response)
		}
	}
	want := []string{"GET /v1/batches/batch_1", "POST /v1/batches/batch_1/cancel", "GET /v1/batches?after=batch_0&limit=10"}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Fatalf("seen=%v want=%v", seen, want)
	}
}

func TestInvokeBatchFileContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/files/file_out/content" {
			t.Fatalf("request=%s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer provider-secret" {
			t.Fatal("missing server-side credential")
		}
		response.Header().Set("Content-Type", "application/octet-stream")
		response.Write([]byte("batch-result\n"))
	}))
	defer server.Close()
	request := &interaction.UnifiedRequest{Kind: interaction.RequestFile, Model: "batch-logical", File: &interaction.FilePayload{ID: "file_out"}}
	result, err := connectorFor(t, server, time.Second).Invoke(context.Background(), contracts.InvocationRequest{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Response.RawContent) != "batch-result\n" {
		t.Fatalf("raw content=%q", result.Response.RawContent)
	}
}

func TestInvokeFileLifecycle(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		seen = append(seen, request.Method+" "+request.URL.RequestURI())
		response.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/v1/files":
			body, _ := io.ReadAll(request.Body)
			if strings.Contains(string(body), `name="model"`) || !strings.Contains(string(body), `name="purpose"`) || !strings.Contains(string(body), `filename="input.jsonl"`) {
				t.Fatalf("upload body=%s", body)
			}
			response.Write([]byte(`{"id":"file_upload","object":"file","filename":"input.jsonl","purpose":"batch"}`))
		case request.Method == http.MethodGet && request.URL.Path == "/v1/files":
			response.Write([]byte(`{"object":"list","data":[{"id":"file_upload","object":"file"}],"has_more":false}`))
		case request.Method == http.MethodGet && request.URL.Path == "/v1/files/file_upload":
			response.Write([]byte(`{"id":"file_upload","object":"file","filename":"input.jsonl"}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/v1/files/file_upload":
			response.Write([]byte(`{"id":"file_upload","object":"file","deleted":true}`))
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.RequestURI())
		}
	}))
	defer server.Close()
	connector := connectorFor(t, server, time.Second)
	requests := []*interaction.UnifiedRequest{
		{Kind: interaction.RequestFile, Model: "batch-logical", File: &interaction.FilePayload{Operation: "upload", Purpose: "batch", Filename: "input.jsonl", Data: []byte("{}\n")}},
		{Kind: interaction.RequestFile, Model: "batch-logical", File: &interaction.FilePayload{Operation: "list", Limit: 10, After: "file_0"}},
		{Kind: interaction.RequestFile, Model: "batch-logical", File: &interaction.FilePayload{Operation: "retrieve", ID: "file_upload"}},
		{Kind: interaction.RequestFile, Model: "batch-logical", File: &interaction.FilePayload{Operation: "delete", ID: "file_upload"}},
	}
	for _, request := range requests {
		result, err := connector.Invoke(context.Background(), contracts.InvocationRequest{Request: request})
		if err != nil {
			t.Fatal(err)
		}
		if result.Response.File == nil {
			t.Fatalf("response=%+v", result.Response)
		}
	}
	want := []string{"POST /v1/files", "GET /v1/files?after=file_0&limit=10", "GET /v1/files/file_upload", "DELETE /v1/files/file_upload"}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Fatalf("seen=%v want=%v", seen, want)
	}
}

func TestHTTPFailuresAndSecretRedaction(t *testing.T) {
	for _, status := range []int{429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				w.Write([]byte("provider-secret"))
			}))
			defer server.Close()
			_, err := connectorFor(t, server, time.Second).Invoke(context.Background(), contracts.InvocationRequest{Request: chatRequest()})
			normalized := connectorFor(t, server, time.Second).NormalizeError(err)
			if normalized.StatusCode != status || !normalized.Retryable || strings.Contains(err.Error(), "provider-secret") {
				t.Fatalf("error=%v normalized=%+v", err, normalized)
			}
		})
	}
}

func TestConnectorResolvesSecretThroughCachingProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer cached-secret" {
			t.Error("missing or wrong server-side credential")
		}
		response.Header().Set("Content-Type", "application/json")
		response.Write([]byte(`{"id":"r","model":"model","created":1,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()
	config := DefaultConfig()
	config.BaseURL = server.URL
	config.SecretRef = "secret://provider"
	config.RequestTimeout = time.Second
	config.AllowInsecureHTTP = true

	// Credential lives behind the caching Secret Provider, never in the request path.
	cached, err := platformsecrets.NewCachingProvider(platformsecrets.NewMemoryProvider(map[string][]byte{"secret://provider": []byte("cached-secret")}), platformsecrets.CacheConfig{TTL: time.Minute}, nil)
	if err != nil {
		t.Fatal(err)
	}
	connector, err := New(config, server.Client(), cached)
	if err != nil {
		t.Fatal(err)
	}
	result, err := connector.Invoke(context.Background(), contracts.InvocationRequest{Request: chatRequest()})
	if err != nil || result.Response.Usage.TotalTokens() != 2 {
		t.Fatalf("Invoke()=%+v,%v", result, err)
	}
	if cached.CacheSize() != 1 {
		t.Fatalf("cache size = %d", cached.CacheSize())
	}
}
func TestTimeoutAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		select {
		case <-request.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
	}))
	defer server.Close()
	connector := connectorFor(t, server, 20*time.Millisecond)
	_, err := connector.Invoke(context.Background(), contracts.InvocationRequest{Request: chatRequest()})
	if err == nil || !connector.NormalizeError(err).Retryable {
		t.Fatalf("timeout error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := connector.Invoke(ctx, contracts.InvocationRequest{Request: chatRequest()}); err == nil {
		t.Fatal("cancelled request succeeded")
	}
}
func TestStreamAndDrop(t *testing.T) {
	complete := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("data: {\"id\":\"r\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer complete.Close()
	output := &writer{}
	if err := connectorFor(t, complete, time.Second).Stream(context.Background(), contracts.InvocationRequest{Request: chatRequest()}, output); err != nil || len(output.events) < 2 {
		t.Fatalf("Stream() events=%+v error=%v", output.events, err)
	}
	dropped := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n"))
	}))
	defer dropped.Close()
	if err := connectorFor(t, dropped, time.Second).Stream(context.Background(), contracts.InvocationRequest{Request: chatRequest()}, &writer{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("dropped Stream() error=%v", err)
	}
}
func TestOpenAICompatibleCustomPath(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = r.URL.Path == "/custom/chat"
		w.Write([]byte(`{"choices":[],"usage":{}}`))
	}))
	defer server.Close()
	config := DefaultConfig()
	config.BaseURL = server.URL
	config.SecretRef = "secret://provider"
	config.ChatPath = "/custom/chat"
	config.AllowInsecureHTTP = true
	connector, err := NewCompatible(config, server.Client(), secrets{"secret://provider": []byte("secret")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connector.Invoke(context.Background(), contracts.InvocationRequest{Request: chatRequest()}); err != nil || !called {
		t.Fatalf("Invoke() called=%t error=%v", called, err)
	}
}
