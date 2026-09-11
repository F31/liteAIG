package a2a

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	wirea2a "github.com/F31/liteAIG/internal/access/protocol/a2a"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

func TestA2AConnectorInvokeAndProvenance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("A2A-Protocol-Version") != "1.0.0" {
			t.Error("missing A2A protocol version")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"message":{"parts":[{"kind":"text","text":"external reply"}]}}}`))
	}))
	defer server.Close()
	connector, err := New(Config{BaseURL: server.URL, Timeout: time.Second}, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := connector.Invoke(context.Background(), contracts.InvocationRequest{RequestID: "r", Target: contracts.TargetRef{ID: "ext-agent"}, Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Chat: &interaction.ChatPayload{}}})
	if err != nil || response.Response.ToolResult.Content != "external reply" {
		t.Fatalf("Invoke() = %+v, %v", response, err)
	}
	// External responses are untrusted provenance.
	if response.Response.ToolResult.Provenance.Source != "external_agent_response" || response.Response.ToolResult.Provenance.Trusted {
		t.Fatalf("provenance = %+v", response.Response.ToolResult.Provenance)
	}
}

func TestA2AConnectorSendsConformantEnvelope(t *testing.T) {
	var decoded struct {
		JSONRPC string `json:"jsonrpc"`
		ID      string `json:"id"`
		Method  string `json:"method"`
		Params  struct {
			Message struct {
				MessageID string `json:"messageId"`
				Role      string `json:"role"`
				Parts     []struct {
					Kind string `json:"kind"`
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"message"`
		} `json:"params"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("A2A-Protocol-Version") != "1.0.0" {
			t.Error("missing A2A protocol version")
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("request is not valid JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"message":{"parts":[{"kind":"text","text":"ok"}]}}}`))
	}))
	defer server.Close()
	connector, err := New(Config{BaseURL: server.URL, Timeout: time.Second}, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "assistant", Content: "hello agent"}, {Role: "user", Content: "relay me"}}}}
	if _, err := connector.Invoke(context.Background(), contracts.InvocationRequest{RequestID: "req-42", Target: contracts.TargetRef{ID: "ext"}, Request: request}); err != nil {
		t.Fatal(err)
	}
	if decoded.JSONRPC != "2.0" || decoded.Method != "message/send" || decoded.ID != "req-42" {
		t.Fatalf("envelope = %+v", decoded)
	}
	message := decoded.Params.Message
	if message.MessageID != "req-42" || message.Role != "user" || len(message.Parts) != 1 || message.Parts[0].Kind != "text" || message.Parts[0].Text != "relay me" {
		t.Fatalf("outbound A2A message = %+v", message)
	}
}

func TestA2AConnectorConcatenatesResponseTextParts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"message":{"parts":[{"kind":"text","text":"a"},{"kind":"text","text":"b"}]}}}`))
	}))
	defer server.Close()
	connector, err := New(Config{BaseURL: server.URL, Timeout: time.Second}, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := connector.Invoke(context.Background(), contracts.InvocationRequest{RequestID: "r", Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "x"}}}}})
	if err != nil || response.Response.ToolResult.Content != "a\nb" {
		t.Fatalf("Invoke() = %+v, %v", response, err)
	}
}

func TestA2AConnectorSurfacesJSONRPCErrorAndNeverRetries(t *testing.T) {
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":"r","error":{"code":-32000,"message":"remote boom"}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		connector, err := New(Config{BaseURL: server.URL, Timeout: time.Second}, server.Client(), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = connector.Invoke(context.Background(), contracts.InvocationRequest{RequestID: "r", Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Chat: &interaction.ChatPayload{}}})
		var upstream *contracts.UpstreamError
		if err == nil || !strings.Contains(err.Error(), "remote boom") {
			t.Fatalf("expected JSON-RPC error, got %v", err)
		}
		if !errorsAsUpstream(err, &upstream) || upstream.Retryable {
			t.Fatalf("A2A error must be non-retryable: %+v", upstream)
		}
		server.Close()
	}
}

func errorsAsUpstream(err error, target **contracts.UpstreamError) bool {
	u, ok := err.(*contracts.UpstreamError)
	if ok {
		*target = u
	}
	return ok
}

func TestA2AConnectorHTTPFailureNeverRetried(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	connector, err := New(Config{BaseURL: server.URL, Timeout: time.Second}, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = connector.Invoke(context.Background(), contracts.InvocationRequest{RequestID: "r", Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Chat: &interaction.ChatPayload{}}})
	var upstream *contracts.UpstreamError
	if err == nil || !errorsAsUpstream(err, &upstream) || upstream.Retryable {
		t.Fatalf("5xx A2A send must be non-retryable: %+v, %v", upstream, err)
	}
}

// chunkSink records every normalized stream chunk a connector emits.
type chunkSink struct {
	chunks []contracts.StreamChunk
}

func (s *chunkSink) WriteChunk(_ context.Context, chunk contracts.StreamChunk) error {
	s.chunks = append(s.chunks, chunk)
	return nil
}

// concatenatedDeltas joins the non-final delta chunks a sink received.
func concatenatedDeltas(sink *chunkSink) string {
	var text strings.Builder
	for _, chunk := range sink.chunks {
		if !chunk.Event.Final {
			text.WriteString(chunk.Event.Delta)
		}
	}
	return text.String()
}

func lastChunk(sink *chunkSink) contracts.StreamChunk {
	if len(sink.chunks) == 0 {
		return contracts.StreamChunk{}
	}
	return sink.chunks[len(sink.chunks)-1]
}

func TestA2AConnectorStreamsSSEDeltas(t *testing.T) {
	var gotAccept bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = strings.Contains(r.Header.Get("Accept"), "text/event-stream")
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, delta := range []string{"Hel", "lo ", "world"} {
			frame, err := wirea2a.EncodeStreamChunk(delta)
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = w.Write([]byte("data: " + string(frame) + "\n\n"))
			flusher.Flush()
		}
		done, _ := wirea2a.EncodeStreamCompleted()
		_, _ = w.Write([]byte("data: " + string(done) + "\n\n"))
		flusher.Flush()
	}))
	defer server.Close()
	connector, err := New(Config{BaseURL: server.URL, Timeout: time.Second}, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sink := &chunkSink{}
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Stream: true, Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "relay"}}}}
	if err := connector.Stream(context.Background(), contracts.InvocationRequest{RequestID: "s-1", Request: request}, sink); err != nil {
		t.Fatal(err)
	}
	if !gotAccept {
		t.Fatal("streaming request did not advertise Accept: text/event-stream")
	}
	if got := concatenatedDeltas(sink); got != "Hello world" {
		t.Fatalf("streamed deltas = %q, want %q", got, "Hello world")
	}
	if final := lastChunk(sink); !final.Event.Final {
		t.Fatalf("stream did not terminate with a Final chunk: %+v", sink.chunks)
	}
	if len(sink.chunks) != 4 {
		t.Fatalf("expected 3 delta chunks + 1 final chunk, got %d", len(sink.chunks))
	}
}

func TestA2AConnectorStreamFallsBackToJSONResult(t *testing.T) {
	// A streaming request answered with a blocking JSON result (peer without
	// the streaming profile) must still succeed as a single final chunk.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"s-1","result":{"message":{"parts":[{"kind":"text","text":"blocking reply"}]}}}`))
	}))
	defer server.Close()
	connector, err := New(Config{BaseURL: server.URL, Timeout: time.Second}, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sink := &chunkSink{}
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Stream: true, Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "relay"}}}}
	if err := connector.Stream(context.Background(), contracts.InvocationRequest{RequestID: "s-1", Request: request}, sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.chunks) != 1 || sink.chunks[0].Event.Delta != "blocking reply" || !sink.chunks[0].Event.Final {
		t.Fatalf("JSON fallback chunks = %+v", sink.chunks)
	}
}

func TestA2AConnectorStreamBlockingRequestStillSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"message":{"parts":[{"kind":"text","text":"whole reply"}]}}}`))
	}))
	defer server.Close()
	connector, err := New(Config{BaseURL: server.URL, Timeout: time.Second}, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sink := &chunkSink{}
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "relay"}}}}
	if err := connector.Stream(context.Background(), contracts.InvocationRequest{RequestID: "s-2", Request: request}, sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.chunks) != 1 || sink.chunks[0].Event.Delta != "whole reply" || !sink.chunks[0].Event.Final {
		t.Fatalf("blocking stream chunks = %+v", sink.chunks)
	}
}

func TestA2AConnectorStreamSurfacesSSERPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","id":"s-1","error":{"code":-32000,"message":"remote stream boom"}}` + "\n\n"))
	}))
	defer server.Close()
	connector, err := New(Config{BaseURL: server.URL, Timeout: time.Second}, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Stream: true, Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "relay"}}}}
	err = connector.Stream(context.Background(), contracts.InvocationRequest{RequestID: "s-1", Request: request}, &chunkSink{})
	if err == nil || !strings.Contains(err.Error(), "remote stream boom") {
		t.Fatalf("expected the SSE JSON-RPC error, got %v", err)
	}
	var upstream *contracts.UpstreamError
	if !errorsAsUpstream(connector.NormalizeError(err), &upstream) || upstream.Retryable {
		t.Fatalf("SSE RPC error must normalize non-retryable: %+v", upstream)
	}
}

func TestA2AConnectorStreamRemote5xxNeverRetried(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	connector, err := New(Config{BaseURL: server.URL, Timeout: time.Second}, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Stream: true, Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "relay"}}}}
	err = connector.Stream(context.Background(), contracts.InvocationRequest{RequestID: "s-1", Request: request}, &chunkSink{})
	var upstream *contracts.UpstreamError
	if err == nil || !errorsAsUpstream(err, &upstream) || upstream.Retryable {
		t.Fatalf("5xx stream send must be non-retryable: %+v, %v", upstream, err)
	}
}

func TestA2AConnectorStreamWithoutCompletedIsTruncated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		frame, _ := wirea2a.EncodeStreamChunk("partial")
		_, _ = w.Write([]byte("data: " + string(frame) + "\n\n"))
	}))
	defer server.Close()
	connector, err := New(Config{BaseURL: server.URL, Timeout: time.Second}, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Stream: true, Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "relay"}}}}
	err = connector.Stream(context.Background(), contracts.InvocationRequest{RequestID: "s-1", Request: request}, &chunkSink{})
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("expected io.ErrUnexpectedEOF for a truncated stream, got %v", err)
	}
}

// stubSecretResolver returns a fixed secret for a single reference.
type stubSecretResolver struct{ value []byte }

func (s stubSecretResolver) Resolve(_ context.Context, _ string) ([]byte, error) { return s.value, nil }

func TestA2AConnectorAttachesBearerSecretAndHeaders(t *testing.T) {
	const secret = "s3cr3t-agent-token"
	var gotAuth, gotHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotHeader = r.Header.Get("X-A2A-Tenant")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"message":{"parts":[{"kind":"text","text":"ok"}]}}}`))
	}))
	defer server.Close()
	connector, err := New(Config{
		BaseURL: server.URL, Timeout: time.Second,
		SecretRef: "local://a2a/cred", AuthScheme: "bearer",
		Headers: map[string]string{"X-A2A-Tenant": "acme"},
	}, server.Client(), stubSecretResolver{value: []byte(secret)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connector.Invoke(context.Background(), contracts.InvocationRequest{RequestID: "r", Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Chat: &interaction.ChatPayload{}}}); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer "+secret {
		t.Fatalf("Authorization = %q, want Bearer secret", gotAuth)
	}
	if gotHeader != "acme" {
		t.Fatalf("X-A2A-Tenant = %q, want acme", gotHeader)
	}
}

func TestA2AConnectorAttachesAPIKeyHeader(t *testing.T) {
	const secret = "s3cr3t-api-key"
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"message":{"parts":[{"kind":"text","text":"ok"}]}}}`))
	}))
	defer server.Close()
	connector, err := New(Config{BaseURL: server.URL, Timeout: time.Second, SecretRef: "local://a2a/cred", AuthScheme: "x-api-key"}, server.Client(), stubSecretResolver{value: []byte(secret)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connector.Invoke(context.Background(), contracts.InvocationRequest{RequestID: "r", Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Chat: &interaction.ChatPayload{}}}); err != nil {
		t.Fatal(err)
	}
	if got != secret {
		t.Fatalf("x-api-key = %q, want secret", got)
	}
}

func TestA2AConnectorSecretNeverLeaksIntoErrors(t *testing.T) {
	const secret = "ultra-secret-a2a-material"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	connector, err := New(Config{BaseURL: server.URL, Timeout: time.Second, SecretRef: "local://a2a/cred", AuthScheme: "bearer"}, server.Client(), stubSecretResolver{value: []byte(secret)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = connector.Invoke(context.Background(), contracts.InvocationRequest{RequestID: "r", Request: &interaction.UnifiedRequest{Kind: interaction.RequestChat, Chat: &interaction.ChatPayload{}}})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked the secret: %v", err)
	}
	if normalized := connector.NormalizeError(err); strings.Contains(normalized.Message, secret) {
		t.Fatalf("normalized error leaked the secret: %+v", normalized)
	}
	// A secret ref without a resolver is a construction error.
	if _, err := New(Config{BaseURL: server.URL, Timeout: time.Second, SecretRef: "local://a2a/cred", AuthScheme: "bearer"}, server.Client(), nil); err == nil {
		t.Fatal("secret ref without a resolver accepted")
	}
}
