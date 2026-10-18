package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controlconfig "github.com/F31/liteAIG/internal/controlplane/config"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

// TestAudioEndToEnd proves the transcription, translation, and speech surfaces
// run through the real Lite gateway: each request is governed by the
// seven-stage pipeline, routed to an audio-capable deployment by capability,
// and the provider response is rendered in the correct wire shape (JSON text
// for STT/translation, raw audio bytes for TTS).
func TestAudioEndToEnd(t *testing.T) {
	provider := newTestProviderServer(t)
	lite, err := NewLite(context.Background(), LiteOptions{DSN: testMemoryDSN(t, "audio-e2e")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lite.Close() })
	admin := httptest.NewServer(lite.Handler())
	t.Cleanup(admin.Close)
	gateway := httptest.NewServer(lite.GatewayHandler())
	t.Cleanup(gateway.Close)

	setupBody := map[string]string{
		"username": "admin", "adminPassword": "password-123456", "tenantName": "Acme",
		"providerName": "Test Provider", "providerType": "openai",
		"providerEndpoint": providerEndpoint(provider), "providerSecret": testProviderKey,
		"selectedModel": "gpt-4o-mini",
	}
	status, body := doJSON(t, admin.URL+"/api/admin/setup", "POST", setupBody, "", "")
	if status != http.StatusOK {
		t.Fatalf("setup status = %d body = %s", status, body)
	}
	var setupResult struct {
		VirtualKey string `json:"virtualKey"`
	}
	if err := json.Unmarshal([]byte(body), &setupResult); err != nil {
		t.Fatal(err)
	}
	key := setupResult.VirtualKey
	if key == "" {
		t.Fatalf("setup did not return a virtual key: %s", body)
	}

	status, loginBody, cookies := doJSONFull(t, admin.URL+"/api/admin/session", "POST", map[string]string{
		"username": "admin", "password": "password-123456",
	}, "")
	if status != http.StatusOK {
		t.Fatalf("login status = %d body = %s", status, loginBody)
	}
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal([]byte(loginBody), &session); err != nil {
		t.Fatal(err)
	}

	// Publish a config where the default deployment also declares the audio and
	// speech capabilities, and expose two audio logical models routed to it.
	{
		status, body := doJSON(t, admin.URL+"/api/admin/config/drafts", "POST", nil, cookies, session.CSRFToken)
		if status != http.StatusOK {
			t.Fatalf("create draft status = %d body = %s", status, body)
		}
		var draft struct {
			ID       string          `json:"ID"`
			Revision int64           `json:"Revision"`
			Config   json.RawMessage `json:"Config"`
		}
		if err := json.Unmarshal([]byte(body), &draft); err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := json.Unmarshal(draft.Config, &document); err != nil {
			t.Fatal(err)
		}
		deployments := document["deployments"].([]any)
		if len(deployments) == 0 {
			t.Fatal("setup produced no deployment")
		}
		deployment := deployments[0].(map[string]any)
		deployment["capabilities"] = []string{"chat", "audio", "speech"}
		updated, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		status, body = doJSON(t, admin.URL+"/api/admin/config/drafts/"+draft.ID, "PUT", map[string]any{"revision": draft.Revision, "config": json.RawMessage(updated)}, cookies, session.CSRFToken)
		if status != http.StatusOK {
			t.Fatalf("update draft status = %d body = %s", status, body)
		}
		var updatedDraft struct {
			Revision int64 `json:"Revision"`
		}
		if err := json.Unmarshal([]byte(body), &updatedDraft); err != nil {
			t.Fatal(err)
		}
		status, body = doJSON(t, admin.URL+"/api/admin/config/drafts/"+draft.ID+"/publish", "POST", map[string]int64{"revision": updatedDraft.Revision}, cookies, session.CSRFToken)
		if status != http.StatusOK {
			t.Fatalf("publish status = %d body = %s", status, body)
		}
	}

	// Text-to-speech returns raw audio bytes with the audio content type.
	status, body = doGateway(t, gateway.URL+"/v1/audio/speech", http.MethodPost, key, map[string]any{
		"model": "default-chat", "input": "hello", "voice": "alloy", "response_format": "mp3",
	})
	if status != http.StatusOK || body != "fake-mp3" {
		t.Fatalf("speech status = %d body = %q, want 200 fake-mp3", status, body)
	}

	// Translation returns the OpenAI text JSON shape.
	status, body = doGatewayMultipart(t, gateway.URL+"/v1/audio/translations", key, "default-chat", []byte("audio-bytes"))
	if status != http.StatusOK || !strings.Contains(body, `"translated"`) {
		t.Fatalf("translation status = %d body = %s, want 200 translated", status, body)
	}

	// Transcription returns the OpenAI text JSON shape.
	status, body = doGatewayMultipart(t, gateway.URL+"/v1/audio/transcriptions", key, "default-chat", []byte("audio-bytes"))
	if status != http.StatusOK || !strings.Contains(body, `"transcribed"`) {
		t.Fatalf("transcription status = %d body = %s, want 200 transcribed", status, body)
	}
}

// TestRequiredCapabilitiesForAudio pins the capability hard constraint each
// audio operation applies during routing.
func TestRequiredCapabilitiesForAudio(t *testing.T) {
	cases := []struct {
		name string
		req  *interaction.UnifiedRequest
		want string
	}{
		{"speech", &interaction.UnifiedRequest{Kind: interaction.RequestAudio, Audio: &interaction.AudioPayload{Operation: interaction.AudioSpeech}}, "speech"},
		{"translation", &interaction.UnifiedRequest{Kind: interaction.RequestAudio, Audio: &interaction.AudioPayload{Operation: interaction.AudioTranslation}}, "audio"},
		{"transcription", &interaction.UnifiedRequest{Kind: interaction.RequestAudio, Audio: &interaction.AudioPayload{Operation: interaction.AudioTranscription}}, "audio"},
		{"legacy-empty", &interaction.UnifiedRequest{Kind: interaction.RequestAudio, Audio: &interaction.AudioPayload{}}, "audio"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := requiredCapabilities(test.req)
			if len(got) != 1 || got[0] != test.want {
				t.Fatalf("requiredCapabilities() = %v, want [%s]", got, test.want)
			}
		})
	}
}

// TestRequiredCapabilitiesEnforceVision proves image-bearing chat requests are
// hard-constrained to deployments that declare the vision capability, while
// text-only chat stays unconstrained (any chat deployment).
func TestRequiredCapabilitiesEnforceVision(t *testing.T) {
	text := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Chat: &interaction.ChatPayload{
		Messages: []interaction.Message{{Role: "user", Content: "hi"}},
	}}
	if got := requiredCapabilities(text); got != nil {
		t.Fatalf("text chat requiredCapabilities() = %v, want nil", got)
	}
	message := interaction.Message{Role: "user", Content: "what is this?"}
	message.AddImage(interaction.ContentPart{MediaType: "image/png", DataURL: "data:image/png;base64,AAAA"})
	image := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Chat: &interaction.ChatPayload{
		Messages: []interaction.Message{message},
	}}
	got := requiredCapabilities(image)
	if len(got) != 1 || got[0] != controlconfig.CapabilityVision {
		t.Fatalf("image chat requiredCapabilities() = %v, want [vision]", got)
	}
}

// doGatewayMultipart posts an OpenAI audio multipart body to the data plane.
func doGatewayMultipart(t *testing.T, url, key, model string, data []byte) (int, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", model)
	part, err := writer.CreateFormFile("file", "sample.wav")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(raw)
}
