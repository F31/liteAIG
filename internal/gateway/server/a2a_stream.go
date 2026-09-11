package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/F31/liteAIG/internal/access/protocol/a2a"
	"github.com/F31/liteAIG/internal/federation"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	kernelerrors "github.com/F31/liteAIG/internal/kernel/errors"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/platform/egress"
)

// ToolStreamer is the optional Core capability that renders an A2A relay result
// as streamed events to an SSE writer. A Core that does not implement it can
// still serve blocking A2A relays; only clients that request the streaming
// profile are affected.
type ToolStreamer interface {
	ToolStream(ctx context.Context, input AdmitInput, writer contracts.StreamWriter) error
}

// PushNotificationConfig is the minimal, best-effort async-delivery extension a
// caller may attach to A2A message/send params:
//
//	{"message": {...}, "pushNotificationConfig": {"url": "...", "token": "..."}}
//
// On a successful non-streaming relay the completed task result is POSTed to
// url (egress-validated, bounded timeout). When the server is wired with an
// outbox the callback is durable, signed, and retried within a bounded budget;
// otherwise the legacy one-shot callback path is used.
type PushNotificationConfig struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// a2aPushTimeout bounds one best-effort push callback POST.
const a2aPushTimeout = 5 * time.Second

const (
	a2aPushDefaultMaxAttempts = 3
	a2aPushBatchSize          = 25
)

// serveA2ARelay runs one normalized inbound A2A message through the Core and
// renders the reply. A caller that asked for the SSE streaming profile (Accept:
// text/event-stream or ?stream=1) receives the LiteAIG A2A streaming events;
// otherwise a blocking JSON-RPC result. When params carry a pushNotificationConfig
// the completed result is additionally posted to that callback best-effort.
func (s *Server) serveA2ARelay(w http.ResponseWriter, r *http.Request, id any, request *interaction.UnifiedRequest, bodyBytes int64, federated *FederatedTransport, push *PushNotificationConfig, v1 bool, streamV1 bool) {
	admit := AdmitInput{Token: bearerToken(r), RemoteIP: remoteIP(r), Protocol: "a2a", BodyBytes: bodyBytes, Request: request, Federated: federated}
	if streamV1 {
		// Official SDK v1.x streaming uses JSON-RPC responses as SSE data frames.
		// Keep it distinct from the documented LiteAIG stream profile so existing
		// Accept: text/event-stream clients remain wire-compatible.
		s.serveA2AStreamV1(w, r, id, admit, request.SessionID)
		return
	}
	if wantsA2AStream(r) {
		// Streaming request: push is only defined for completed async results,
		// so it is deliberately ignored on this path.
		s.serveA2AStream(w, r, id, admit)
		return
	}
	response, err := s.core.Tool(r.Context(), admit)
	if err != nil {
		writeA2AError(w, id, err)
		return
	}
	content := ""
	if response != nil && response.ToolResult != nil {
		content = response.ToolResult.Content
	}
	var result map[string]any
	if v1 {
		// A2A 1.0 wire (official SDK v1.x): the result is a protobuf-JSON
		// SendMessageResponse carrying a single oneof payload. A completed
		// blocking relay answers with a message whose text part carries the
		// relayed reply; unknown/0.3 part keys are absent so the SDK's strict
		// ParseDict accepts it.
		messageID := request.SessionID
		if messageID == "" {
			messageID = "liteaig-" + hex.EncodeToString(mustRandomBytes(8))
		}
		result = map[string]any{
			"message": map[string]any{
				"messageId": messageID,
				"role":      "ROLE_AGENT",
				"parts":     []map[string]string{{"text": content}},
			},
		}
	} else {
		result = map[string]any{"message": map[string]any{"parts": []map[string]string{{"kind": "text", "text": content}}}}
	}
	writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	if push != nil && push.URL != "" {
		taskID := ""
		if response != nil {
			taskID = response.ID
		}
		s.deliverA2APush(push.URL, push.Token, id, request.SessionID, taskID, result)
	}
}

// wantsA2AStream reports whether the inbound A2A caller asked for the SSE
// streaming profile (docs/A2A_STREAMING.md) via the Accept header or ?stream=1.
func wantsA2AStream(r *http.Request) bool {
	if r.URL.Query().Get("stream") == "1" {
		return true
	}
	for _, value := range r.Header.Values("Accept") {
		if strings.Contains(strings.ToLower(value), "text/event-stream") {
			return true
		}
	}
	return false
}

// serveA2AStream answers a streaming A2A request. The reply is the LiteAIG A2A
// SSE profile: one message event carrying the completed agent reply followed by
// a completed event (and connection close). Because the Lite relay resolves to a
// completed agent reply, the message event currently arrives as a single frame;
// future incremental inbound deltas would flow through the same event types.
func (s *Server) serveA2AStream(w http.ResponseWriter, r *http.Request, id any, admit AdmitInput) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeA2AError(w, id, &kernelerrors.Error{Code: "STREAMING_UNSUPPORTED", Message: "streaming is not supported by this server"})
		return
	}
	streamer, ok := s.core.(ToolStreamer)
	if !ok {
		writeA2AError(w, id, &kernelerrors.Error{Code: "STREAMING_UNSUPPORTED", Message: "streaming is not supported for A2A by this server"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	writer := newSSEWriter(w, flusher, a2aStreamEncoder{})
	if err := streamer.ToolStream(r.Context(), admit, writer); err != nil {
		_ = writer.fail(err)
	}
}

// serveA2AStreamV1 answers the official SDK 1.0 SendStreamingMessage method.
// The SDK's JSON-RPC transport parses each SSE data frame as a JSON-RPC response
// whose result is a StreamResponse protobuf-JSON object.
func (s *Server) serveA2AStreamV1(w http.ResponseWriter, r *http.Request, id any, admit AdmitInput, messageID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeA2AError(w, id, &kernelerrors.Error{Code: "STREAMING_UNSUPPORTED", Message: "streaming is not supported by this server"})
		return
	}
	streamer, ok := s.core.(ToolStreamer)
	if !ok {
		writeA2AError(w, id, &kernelerrors.Error{Code: "STREAMING_UNSUPPORTED", Message: "streaming is not supported for A2A by this server"})
		return
	}
	if messageID == "" {
		messageID = "liteaig-" + hex.EncodeToString(mustRandomBytes(8))
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	writer := newSSEWriter(w, flusher, a2aV1StreamEncoder{id: id, messageID: messageID})
	if err := streamer.ToolStream(r.Context(), admit, writer); err != nil {
		_ = writer.fail(err)
	}
}

// a2aStreamEncoder renders normalized interaction stream events as LiteAIG A2A
// SSE data frames (see docs/A2A_STREAMING.md): one {"type":"message",...} frame
// per text delta and a terminal {"type":"completed"} frame on the final event.
type a2aStreamEncoder struct{}

func (a2aStreamEncoder) encode(event interaction.StreamEvent) ([]sseFrame, error) {
	if event.Delta == "" {
		return nil, nil
	}
	data, err := a2a.EncodeStreamChunk(event.Delta)
	if err != nil {
		return nil, err
	}
	return []sseFrame{{data: data}}, nil
}

func (a2aStreamEncoder) final() []sseFrame {
	data, err := a2a.EncodeStreamCompleted()
	if err != nil {
		return nil
	}
	return []sseFrame{{data: data}}
}

type a2aV1StreamEncoder struct {
	id        any
	messageID string
}

func (e a2aV1StreamEncoder) encode(event interaction.StreamEvent) ([]sseFrame, error) {
	if event.Delta == "" {
		return nil, nil
	}
	return e.frame(map[string]any{
		"message": map[string]any{
			"messageId": e.messageID,
			"role":      "ROLE_AGENT",
			"parts":     []map[string]string{{"text": event.Delta}},
		},
	})
}

func (e a2aV1StreamEncoder) final() []sseFrame {
	frames, err := e.frame(map[string]any{
		"statusUpdate": map[string]any{
			"taskId": e.messageID,
			"status": map[string]string{"state": "TASK_STATE_COMPLETED"},
		},
	})
	if err != nil {
		return nil
	}
	return frames
}

func (e a2aV1StreamEncoder) frame(result map[string]any) ([]sseFrame, error) {
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": e.id, "result": result})
	if err != nil {
		return nil, err
	}
	return []sseFrame{{data: data}}, nil
}

// deliverA2APush records (or directly sends) the completed result of a
// successful non-streaming A2A relay to the caller-supplied callback URL. URLs
// and tokens are never written to logs.
func (s *Server) deliverA2APush(url, token string, id any, messageID, taskID string, result map[string]any) {
	if egress.ValidateTarget(url, egress.LitePolicy()) != nil {
		s.logf("a2a push skipped: callback target rejected by egress policy")
		return
	}
	body := map[string]any{"jsonrpc": "2.0", "id": id, "messageId": messageID, "result": result}
	if taskID != "" {
		body["task"] = map[string]any{"id": taskID, "status": "completed"}
	} else {
		body["task"] = map[string]any{"status": "completed"}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		s.logf("a2a push failed: encode")
		return
	}
	if s.cfg.A2APushOutbox != nil && taskID != "" {
		storedURL := url
		if s.cfg.A2APushSealURL != nil {
			sealed, err := s.cfg.A2APushSealURL(url)
			if err != nil {
				s.logf("a2a push failed: seal url")
				return
			}
			storedURL = sealed
		}
		storedToken := token
		if storedToken != "" && s.cfg.A2APushSealBearer != nil {
			sealed, err := s.cfg.A2APushSealBearer([]byte(storedToken))
			if err != nil {
				s.logf("a2a push failed: seal token")
				return
			}
			storedToken = sealed
		}
		storedPayload := payload
		if s.cfg.A2APushSealPayload != nil {
			sealed, err := s.cfg.A2APushSealPayload(payload)
			if err != nil {
				s.logf("a2a push failed: seal payload")
				return
			}
			storedPayload = sealed
		}
		if err := s.cfg.A2APushOutbox.EnqueueForTask(context.Background(), federation.A2APushDelivery{
			ID: randomPushID(taskID), TaskID: taskID, CallbackURL: storedURL, BearerToken: storedToken,
			Payload: storedPayload, MaxAttempts: a2aPushDefaultMaxAttempts,
		}); err != nil {
			s.logf("a2a push failed: enqueue")
		}
		return
	}
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logf("a2a push panic: %v", recovered)
			}
		}()
		if ok, reason := s.sendA2APush(context.Background(), federation.A2APushDelivery{CallbackURL: url, BearerToken: token, Payload: payload}); !ok {
			s.logf("a2a push failed: %s", reason)
		}
	}()
}

// RunA2APushWorker drains the durable push outbox until ctx is canceled. It is
// safe to run with a nil outbox; the method then returns immediately.
func (s *Server) RunA2APushWorker(ctx context.Context, interval time.Duration) {
	if s.cfg.A2APushOutbox == nil {
		return
	}
	if interval <= 0 {
		interval = time.Second
	}
	s.DrainA2APushOutbox(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.DrainA2APushOutbox(ctx)
		}
	}
}

func (s *Server) DrainA2APushOutbox(ctx context.Context) {
	if s.cfg.A2APushOutbox == nil {
		return
	}
	deliveries, err := s.cfg.A2APushOutbox.Due(ctx, a2aPushBatchSize, time.Now().UTC())
	if err != nil {
		s.logf("a2a push failed: outbox scan")
		return
	}
	for _, delivery := range deliveries {
		if ctx.Err() != nil {
			return
		}
		ok, reason := s.sendA2APush(ctx, delivery)
		if ok {
			if err := s.cfg.A2APushOutbox.MarkDelivered(ctx, delivery.ID); err != nil {
				s.logf("a2a push failed: mark delivered")
			}
			continue
		}
		attempt := delivery.Attempts + 1
		exhausted := attempt >= delivery.MaxAttempts
		next := time.Now().UTC().Add(a2aPushBackoff(attempt))
		if err := s.cfg.A2APushOutbox.MarkAttempt(ctx, delivery.ID, next, exhausted, reason); err != nil {
			s.logf("a2a push failed: mark retry")
		}
	}
}

func (s *Server) sendA2APush(ctx context.Context, delivery federation.A2APushDelivery) (bool, string) {
	callbackURL := delivery.CallbackURL
	if strings.HasPrefix(callbackURL, "sealed:") && s.cfg.A2APushOpenURL != nil {
		plain, err := s.cfg.A2APushOpenURL(callbackURL)
		if err != nil {
			return false, "url decrypt"
		}
		callbackURL = plain
	}
	if egress.ValidateTarget(callbackURL, egress.LitePolicy()) != nil {
		return false, "target rejected"
	}
	payload := delivery.Payload
	if len(payload) > 0 && bytes.HasPrefix(payload, []byte("sealed:")) && s.cfg.A2APushOpenPayload != nil {
		plain, err := s.cfg.A2APushOpenPayload(payload)
		if err != nil {
			return false, "payload decrypt"
		}
		payload = plain
	}
	callCtx, cancel := context.WithTimeout(ctx, a2aPushTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, callbackURL, bytes.NewReader(payload))
	if err != nil {
		return false, "request"
	}
	request.Header.Set("Content-Type", "application/json")
	if delivery.ID != "" {
		request.Header.Set("X-LiteAIG-A2A-Push-ID", delivery.ID)
	}
	bearer := delivery.BearerToken
	if bearer != "" && strings.HasPrefix(bearer, "sealed:") && s.cfg.A2APushOpenBearer != nil {
		plain, err := s.cfg.A2APushOpenBearer(bearer)
		if err != nil {
			return false, "token decrypt"
		}
		bearer = string(plain)
	}
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	if len(s.cfg.A2APushSigningSecret) > 0 {
		timestamp := time.Now().UTC().Format(time.RFC3339Nano)
		request.Header.Set("X-LiteAIG-A2A-Push-Timestamp", timestamp)
		request.Header.Set("X-LiteAIG-A2A-Push-Signature", "sha256="+a2a.SignPushPayloadWithID(s.cfg.A2APushSigningSecret, delivery.ID, timestamp, payload))
	}
	response, err := s.callbackClient().Do(request)
	if err != nil {
		return false, "callback unreachable"
	}
	response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, fmt.Sprintf("callback status %d", response.StatusCode)
	}
	return true, ""
}

func a2aPushBackoff(attempt int) time.Duration {
	if attempt <= 1 {
		return time.Second
	}
	if attempt == 2 {
		return 5 * time.Second
	}
	return 30 * time.Second
}

func randomPushID(taskID string) string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return taskID + ".push." + hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("%s.push.%d", taskID, time.Now().UnixNano())
}

// mustRandomBytes returns n random bytes, falling back to an hour-granular
// timestamp so the A2A messageId generator never panics under entropy failure.
func mustRandomBytes(n int) []byte {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		for i := range raw {
			raw[i] = byte(time.Now().UnixNano() >> (8 * (i % 8)))
		}
	}
	return raw
}
