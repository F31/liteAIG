package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testProviderKey = "sk-test-provider"

// newTestProviderServer is a minimal OpenAI-compatible provider used by tests:
// real HTTP, real JSON, real SSE. It is a network-boundary double for the
// pipeline — every byte crossing it is wire-format.
func newTestProviderServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o-mini","object":"model"},{"id":"gpt-4o","object":"model"}]}`))
	})
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		var request struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		// Judge support: the pipeline's LLM judge sends a system message that
		// names the "output-quality judge" role. The double plays the judge
		// model and returns a deterministic verdict: it fails only when the
		// reviewed content leaks a secret marker.
		reviewed := ""
		isJudge := false
		for _, message := range request.Messages {
			text := providerContentText(message.Content)
			reviewed += text + "\n"
			if message.Role == "system" && strings.Contains(text, "output-quality judge") {
				isJudge = true
			}
		}
		if isJudge {
			verdict := `{"passed":true,"reason":"response within rubric","score":0.9}`
			if strings.Contains(reviewed, "EXPOSED-SECRET") {
				verdict = `{"passed":false,"reason":"response exposes a secret","score":0.1}`
			}
			// The verdict is returned as a JSON *string* in content, exactly as
			// a real judge model would emit it; the gateway parses it out.
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"id":"chatcmpl-1","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`, request.Model, mustJSON(verdict))
			return
		}
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"stream-\"}}]}\n\n", request.Model)
			if flusher != nil {
				flusher.Flush()
			}
			fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":5,\"total_tokens\":8}}\n\n", request.Model)
			fmt.Fprint(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
		// Normal chat: the echo reflects a leaked-secret marker in the prompt
		// so tests can drive a judge-failing completion.
		assistant := "provider-echo"
		if strings.Contains(reviewed, "EXPOSED-SECRET") {
			assistant = "provider-echo: EXPOSED-SECRET"
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"chatcmpl-1","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`, request.Model, mustJSON(assistant))
	})
	mux.HandleFunc("POST /v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		var request struct {
			Model  string   `json:"model"`
			Inputs []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		// Deterministic keyword embedding: prompts about a "capital" share a
		// vector, everything else maps to an orthogonal one. This makes
		// semantic-cache hit/miss assertions reproducible.
		data := make([]map[string]any, 0, len(request.Inputs))
		for index, input := range request.Inputs {
			embedding := []float64{0, 1, 0, 0}
			if strings.Contains(input, "capital") {
				embedding = []float64{1, 0, 0, 0}
			}
			data = append(data, map[string]any{"object": "embedding", "index": index, "embedding": embedding})
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"object":"list","data":%s,"model":%q,"usage":{"prompt_tokens":1,"total_tokens":1}}`, mustJSON(data), request.Model)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// providerContentText extracts the plain-text portion of an OpenAI chat
// message content, which is either a plain string (text-only messages) or an
// array of {"type":"text"|"image_url",...} parts (multimodal messages). The
// fake's downstream checks only ever read prompt text, so image parts are
// ignored and text parts are concatenated. Text-only content decodes
// byte-identically to the original string field.
func providerContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var plain string
	if err := json.Unmarshal(raw, &plain); err == nil {
		return plain
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	var text strings.Builder
	for _, part := range parts {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
	}
	return text.String()
}

func mustJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if got := r.Header.Get("Authorization"); got != "Bearer "+testProviderKey {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key","type":"invalid_request_error","code":"invalid_api_key"}}`))
		return false
	}
	return true
}

// providerEndpoint returns the bare base URL: connectors append /v1/... paths
// themselves and the wizard probe calls endpoint+/v1/models.
func providerEndpoint(server *httptest.Server) string {
	return server.URL
}
