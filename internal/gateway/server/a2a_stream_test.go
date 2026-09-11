package server

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
	"github.com/F31/liteAIG/internal/access/protocol/openai"
	"github.com/F31/liteAIG/internal/federation"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	kernelerrors "github.com/F31/liteAIG/internal/kernel/errors"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

// relayCore answers /a2a relays with a fixed text result. It also implements
// ToolStream (ToolStreamer) so streaming-profile requests can be exercised.
type relayCore struct {
	content string
}

func (c *relayCore) Admit(context.Context, AdmitInput) (*kernel.RequestContext, error) {
	return nil, nil
}
func (c *relayCore) Run(context.Context, *kernel.RequestContext, contracts.StreamWriter) error {
	return nil
}
func (c *relayCore) Models(context.Context, string, string) (*openai.ModelsResponse, error) {
	return &openai.ModelsResponse{}, nil
}
func (c *relayCore) Tool(_ context.Context, input AdmitInput) (*interaction.UnifiedResponse, error) {
	if input.Protocol == "a2a" {
		return &interaction.UnifiedResponse{ID: "task-relay", ToolResult: &interaction.ToolResult{Content: c.content}}, nil
	}
	return nil, &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "only a2a is served by relayCore"}
}
func (c *relayCore) ToolStream(ctx context.Context, input AdmitInput, writer contracts.StreamWriter) error {
	response, err := c.Tool(ctx, input)
	if err != nil {
		return err
	}
	if response.ToolResult != nil && response.ToolResult.Content != "" {
		if err := writer.WriteChunk(ctx, contracts.StreamChunk{Event: interaction.StreamEvent{Delta: response.ToolResult.Content}}); err != nil {
			return err
		}
	}
	return writer.WriteChunk(ctx, contracts.StreamChunk{Event: interaction.StreamEvent{Final: true}})
}

const a2aMessageSendBody = `{"jsonrpc":"2.0","id":"3","method":"message/send","params":{"message":{"messageId":"m1","role":"user","parts":[{"kind":"text","text":"hello"}]}}}`

func TestA2ASendMessageV1MethodReturnsV1Wire(t *testing.T) {
	core := &relayCore{content: "v1 relay reply"}
	server := httptest.NewServer(New(core, Config{MaxBodyBytes: 2048}).Handler())
	defer server.Close()
	// Official SDK v1.x method name and protobuf-JSON params (ROLE_USER enum,
	// bare text part without the 0.3 `kind` discriminator).
	body := `{"jsonrpc":"2.0","id":"7","method":"SendMessage","params":{"message":{"messageId":"m-v1","role":"ROLE_USER","parts":[{"text":"hello 1.0"}]}}}`
	resp, responseBody := postWithHeaders(t, server.URL+"/a2a", body, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, responseBody)
	}
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(responseBody), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.JSONRPC != "2.0" || envelope.ID != "7" {
		t.Fatalf("envelope = %+v", envelope)
	}
	var result struct {
		Message struct {
			MessageID string `json:"messageId"`
			Role      string `json:"role"`
			Parts     []struct {
				Text string `json:"text"`
				Kind string `json:"kind"`
			} `json:"parts"`
		} `json:"message"`
		Task any `json:"task"`
	}
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.Task != nil {
		t.Fatalf("v1 wire must carry a message (oneof), got task: %+v", result.Task)
	}
	if result.Message.Role != "ROLE_AGENT" {
		t.Fatalf("role = %q, want ROLE_AGENT", result.Message.Role)
	}
	if len(result.Message.Parts) != 1 || result.Message.Parts[0].Text != "v1 relay reply" {
		t.Fatalf("parts = %+v", result.Message.Parts)
	}
	if result.Message.Parts[0].Kind != "" {
		t.Fatalf("v1 wire leaked 0.3 kind discriminator: %+v", result.Message.Parts[0])
	}
	// The 0.3 method must still produce the legacy part shape.
	resp03, body03 := postWithHeaders(t, server.URL+"/a2a", a2aMessageSendBody, nil)
	if resp03.StatusCode != http.StatusOK || !strings.Contains(body03, `"kind":"text"`) {
		t.Fatalf("0.3 response regression status=%d body=%s", resp03.StatusCode, body03)
	}
}

func TestA2AStreamingProfileEmitsSSEFrames(t *testing.T) {
	core := &relayCore{content: "a2a stream reply"}
	server := httptest.NewServer(New(core, Config{MaxBodyBytes: 2048}).Handler())
	defer server.Close()

	for _, tc := range []struct {
		name    string
		headers map[string]string
		url     string
	}{
		{name: "accept_header", headers: map[string]string{"Accept": "text/event-stream"}},
		{name: "query_stream", url: "?stream=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := postWithHeaders(t, server.URL+"/a2a"+tc.url, a2aMessageSendBody, tc.headers)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status=%d body=%s", resp.StatusCode, body)
			}
			if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
				t.Fatalf("Content-Type = %q, want text/event-stream", resp.Header.Get("Content-Type"))
			}
			messageFrame := `data: {"type":"message","message":{"parts":[{"kind":"text","text":"a2a stream reply"}]}}`
			completedFrame := `data: {"type":"completed"}`
			if !strings.Contains(body, messageFrame) {
				t.Fatalf("stream missing the message frame:\n%s", body)
			}
			if !strings.Contains(body, completedFrame) {
				t.Fatalf("stream missing the completed frame:\n%s", body)
			}
			if strings.Index(body, messageFrame) > strings.Index(body, completedFrame) {
				t.Fatalf("completed frame arrived before the message frame:\n%s", body)
			}
		})
	}
}

func TestA2ASendStreamingMessageV1EmitsSDKParsableSSEFrames(t *testing.T) {
	core := &relayCore{content: "v1 streamed reply"}
	server := httptest.NewServer(New(core, Config{MaxBodyBytes: 2048}).Handler())
	defer server.Close()

	body := `{"jsonrpc":"2.0","id":"9","method":"SendStreamingMessage","params":{"message":{"messageId":"m-v1-stream","role":"ROLE_USER","parts":[{"text":"hello stream 1.0"}]}}}`
	resp, responseBody := postWithHeaders(t, server.URL+"/a2a", body, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, responseBody)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", resp.Header.Get("Content-Type"))
	}
	if strings.Contains(responseBody, `"type":"message"`) {
		t.Fatalf("official v1 stream leaked LiteAIG profile frames:\n%s", responseBody)
	}
	messageFrame := `data: {"id":"9","jsonrpc":"2.0","result":{"message":{"messageId":"m-v1-stream","parts":[{"text":"v1 streamed reply"}],"role":"ROLE_AGENT"}}}`
	completedFrame := `data: {"id":"9","jsonrpc":"2.0","result":{"statusUpdate":{"status":{"state":"TASK_STATE_COMPLETED"},"taskId":"m-v1-stream"}}}`
	if !strings.Contains(responseBody, messageFrame) {
		t.Fatalf("stream missing the SDK message frame:\n%s", responseBody)
	}
	if !strings.Contains(responseBody, completedFrame) {
		t.Fatalf("stream missing the SDK completed frame:\n%s", responseBody)
	}
}

func TestA2AStreamingUnsupportedWhenCoreLacksToolStream(t *testing.T) {
	// fakeCore does not implement ToolStream, so a streaming-profile request is
	// answered with STREAMING_UNSUPPORTED instead of an empty stream.
	server := httptest.NewServer(New(&fakeCore{}, Config{MaxBodyBytes: 2048}).Handler())
	defer server.Close()
	resp, body := postWithHeaders(t, server.URL+"/a2a", a2aMessageSendBody, map[string]string{"Accept": "text/event-stream"})
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s, want 503", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"code":"STREAMING_UNSUPPORTED"`) {
		t.Fatalf("body missing STREAMING_UNSUPPORTED:\n%s", body)
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal("unsupported streaming must not start an SSE body")
	}
}

// callbackHit records one push-callback POST observed by the httptest peer.
type callbackHit struct {
	authorization string
	deliveryID    string
	timestamp     string
	signature     string
	body          string
}

func newCallbackServer(t *testing.T, hits chan callbackHit) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		select {
		case hits <- callbackHit{authorization: r.Header.Get("Authorization"), deliveryID: r.Header.Get("X-LiteAIG-A2A-Push-ID"), timestamp: r.Header.Get("X-LiteAIG-A2A-Push-Timestamp"), signature: r.Header.Get("X-LiteAIG-A2A-Push-Signature"), body: string(raw)}:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

func pushBody(url string, messageID string) string {
	return `{"jsonrpc":"2.0","id":"3","method":"message/send","params":{"message":{"messageId":"` + messageID + `","role":"user","parts":[{"kind":"text","text":"hello"}]},"pushNotificationConfig":{"url":"` + url + `","token":"cb-token"}}}`
}

func TestA2APushCallbackReceivesCompletedResult(t *testing.T) {
	hits := make(chan callbackHit, 1)
	callback := newCallbackServer(t, hits)
	defer callback.Close()

	core := &relayCore{content: "push me the reply"}
	server := httptest.NewServer(New(core, Config{MaxBodyBytes: 4096}).Handler())
	defer server.Close()

	resp, body := postWithHeaders(t, server.URL+"/a2a", pushBody(callback.URL, "m-push"), nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "push me the reply") {
		t.Fatalf("a2a relay status=%d body=%s, want 200 + reply", resp.StatusCode, body)
	}

	select {
	case hit := <-hits:
		if hit.authorization != "Bearer cb-token" {
			t.Fatalf("callback Authorization = %q, want Bearer cb-token", hit.authorization)
		}
		var envelope struct {
			MessageID string `json:"messageId"`
			Result    struct {
				Message struct {
					Parts []struct {
						Kind string `json:"kind"`
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"message"`
			} `json:"result"`
			Task struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"task"`
		}
		if err := json.Unmarshal([]byte(hit.body), &envelope); err != nil {
			t.Fatalf("callback body is not JSON: %v\n%s", err, hit.body)
		}
		if envelope.MessageID != "m-push" {
			t.Fatalf("callback messageId = %q", envelope.MessageID)
		}
		if len(envelope.Result.Message.Parts) != 1 || envelope.Result.Message.Parts[0].Text != "push me the reply" {
			t.Fatalf("callback result parts = %+v", envelope.Result.Message.Parts)
		}
		if envelope.Task.Status != "completed" {
			t.Fatalf("callback task status = %q, want completed", envelope.Task.Status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("push callback was not delivered")
	}
}

type fakePushOutbox struct {
	queued []federation.A2APushDelivery
}

func (f *fakePushOutbox) EnqueueForTask(_ context.Context, delivery federation.A2APushDelivery) error {
	f.queued = append(f.queued, delivery)
	return nil
}

func (f *fakePushOutbox) Due(context.Context, int, time.Time) ([]federation.A2APushDelivery, error) {
	due := append([]federation.A2APushDelivery(nil), f.queued...)
	f.queued = nil
	return due, nil
}

func (f *fakePushOutbox) MarkDelivered(context.Context, string) error { return nil }
func (f *fakePushOutbox) MarkAttempt(context.Context, string, time.Time, bool, string) error {
	return nil
}

func TestA2ADurablePushSealsBearerBeforeOutbox(t *testing.T) {
	hits := make(chan callbackHit, 1)
	callback := newCallbackServer(t, hits)
	defer callback.Close()
	outbox := &fakePushOutbox{}
	core := &relayCore{content: "push me securely"}
	server := New(core, Config{
		MaxBodyBytes:         4096,
		A2APushOutbox:        outbox,
		A2APushSigningSecret: []byte("push-signing-secret"),
		A2APushSealURL: func(value string) (string, error) {
			if value != callback.URL {
				t.Fatalf("seal got url %q", value)
			}
			return "sealed:callback-url", nil
		},
		A2APushOpenURL: func(value string) (string, error) {
			if value != "sealed:callback-url" {
				t.Fatalf("open got url %q", value)
			}
			return callback.URL, nil
		},
		A2APushSealBearer: func(plain []byte) (string, error) {
			if string(plain) != "cb-token" {
				t.Fatalf("seal got token %q", string(plain))
			}
			return "sealed:nekot-bc", nil
		},
		A2APushOpenBearer: func(value string) ([]byte, error) {
			if value != "sealed:nekot-bc" {
				t.Fatalf("open got token %q", value)
			}
			return []byte("cb-token"), nil
		},
		A2APushSealPayload: func(plain []byte) ([]byte, error) {
			if !strings.Contains(string(plain), "push me securely") {
				t.Fatalf("seal got payload %q", string(plain))
			}
			return []byte("sealed:payload"), nil
		},
		A2APushOpenPayload: func(value []byte) ([]byte, error) {
			if string(value) != "sealed:payload" {
				t.Fatalf("open got payload %q", string(value))
			}
			return []byte(`{"jsonrpc":"2.0","id":"3","messageId":"m-secure","result":{"message":{"parts":[{"kind":"text","text":"push me securely"}]}},"task":{"id":"task-relay","status":"completed"}}`), nil
		},
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, body := postWithHeaders(t, httpServer.URL+"/a2a", pushBody(callback.URL, "m-secure"), nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "push me securely") {
		t.Fatalf("a2a relay status=%d body=%s", resp.StatusCode, body)
	}
	if len(outbox.queued) != 1 {
		t.Fatalf("queued deliveries = %d, want 1", len(outbox.queued))
	}
	if outbox.queued[0].CallbackURL == callback.URL || outbox.queued[0].CallbackURL != "sealed:callback-url" {
		t.Fatalf("outbox stored callback url = %q", outbox.queued[0].CallbackURL)
	}
	if outbox.queued[0].BearerToken == "cb-token" || outbox.queued[0].BearerToken != "sealed:nekot-bc" {
		t.Fatalf("outbox stored bearer = %q", outbox.queued[0].BearerToken)
	}
	if string(outbox.queued[0].Payload) == "" || strings.Contains(string(outbox.queued[0].Payload), "push me securely") || string(outbox.queued[0].Payload) != "sealed:payload" {
		t.Fatalf("outbox stored payload = %q", string(outbox.queued[0].Payload))
	}

	server.DrainA2APushOutbox(context.Background())
	select {
	case hit := <-hits:
		if hit.authorization != "Bearer cb-token" {
			t.Fatalf("callback Authorization = %q, want Bearer cb-token", hit.authorization)
		}
		if hit.deliveryID == "" {
			t.Fatal("callback missing delivery id")
		}
		if err := wirea2a.VerifyPushPayloadSignatureWithID([]byte("push-signing-secret"), hit.deliveryID, hit.timestamp, hit.signature, []byte(hit.body), time.Now(), time.Minute); err != nil {
			t.Fatalf("callback signature did not verify: %v", err)
		}
		if !strings.Contains(hit.body, "push me securely") {
			t.Fatalf("callback body = %s", hit.body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("sealed durable push was not delivered")
	}
}

func TestA2APushInvalidTargetNeverFailsCaller(t *testing.T) {
	hits := make(chan callbackHit, 1)
	callback := newCallbackServer(t, hits)
	defer callback.Close()

	core := &relayCore{content: "ok regardless"}
	server := httptest.NewServer(New(core, Config{MaxBodyBytes: 4096}).Handler())
	defer server.Close()

	// A private, non-loopback callback URL fails egress validation: no POST is
	// attempted and the caller still gets a clean 200 reply.
	resp, body := postWithHeaders(t, server.URL+"/a2a", pushBody("http://192.168.55.55/cb", "m-invalid"), nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "ok regardless") {
		t.Fatalf("a2a relay status=%d body=%s, want 200 + reply", resp.StatusCode, body)
	}
	// And a valid loopback callback attached to a STREAMING request must be
	// ignored (push only applies to completed non-streaming results).
	core2 := &relayCore{content: "streamed"}
	server2 := httptest.NewServer(New(core2, Config{MaxBodyBytes: 4096}).Handler())
	defer server2.Close()
	_, sseBody := postWithHeaders(t, server2.URL+"/a2a", pushBody(callback.URL, "m-stream"), map[string]string{"Accept": "text/event-stream"})
	if !strings.Contains(sseBody, "streamed") || !strings.Contains(sseBody, "completed") {
		t.Fatalf("streaming body missing frames:\n%s", sseBody)
	}
	select {
	case hit := <-hits:
		t.Fatalf("push fired on a streaming request: %+v", hit)
	case <-time.After(300 * time.Millisecond):
	}
}
