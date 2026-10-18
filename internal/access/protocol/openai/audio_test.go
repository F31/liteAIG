package openai

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

func TestDecodeEncodeAudioTranscription(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", "whisper-1")
	_ = writer.WriteField("language", "en")
	_ = writer.WriteField("prompt", "names")
	_ = writer.WriteField("temperature", "0.2")
	part, err := writer.CreateFormFile("file", "sample.wav")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("audio-bytes"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := DecodeAudioTranscription(writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	if request.Kind != interaction.RequestAudio || request.Audio.Operation != interaction.AudioTranscription || request.Model != "whisper-1" || request.Audio.Filename != "sample.wav" || string(request.Audio.Data) != "audio-bytes" || *request.Audio.Temperature != 0.2 {
		t.Fatalf("request=%+v audio=%+v", request, request.Audio)
	}
	encoded, contentType, err := EncodeAudioTranscription(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(contentType, "multipart/form-data") || !bytes.Contains(encoded, []byte("audio-bytes")) || !bytes.Contains(encoded, []byte("whisper-1")) {
		t.Fatalf("contentType=%s encoded=%s", contentType, encoded)
	}
}

func TestDecodeEncodeAudioTranscriptionResponse(t *testing.T) {
	response, err := DecodeAudioTranscriptionResponse(strings.NewReader(`{"text":"hello world"}`))
	if err != nil {
		t.Fatal(err)
	}
	if response.Audio.Text != "hello world" || response.Choices[0].Message.Content != "hello world" {
		t.Fatalf("response=%+v", response)
	}
	encoded, err := EncodeAudioTranscriptionResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"text":"hello world"}` {
		t.Fatalf("encoded=%s", encoded)
	}
}

func TestDecodeAudioTranslation(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", "whisper-1")
	part, err := writer.CreateFormFile("file", "sample.wav")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("audio-bytes"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := DecodeAudioTranslation(writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	if request.Kind != interaction.RequestAudio || request.Audio.Operation != interaction.AudioTranslation || request.Model != "whisper-1" || string(request.Audio.Data) != "audio-bytes" {
		t.Fatalf("request=%+v audio=%+v", request, request.Audio)
	}
	encoded, contentType, err := EncodeAudioTranslation(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(contentType, "multipart/form-data") || !bytes.Contains(encoded, []byte("audio-bytes")) {
		t.Fatalf("contentType=%s encoded=%s", contentType, encoded)
	}
}

func TestDecodeEncodeAudioSpeech(t *testing.T) {
	wire := `{"model":"tts-1","input":"hello","voice":"alloy","response_format":"mp3","speed":1.25,"instructions":"calm"}`
	request, err := DecodeAudioSpeech([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	if request.Kind != interaction.RequestAudio || request.Audio.Operation != interaction.AudioSpeech || request.Model != "tts-1" || request.Audio.Input != "hello" || request.Audio.Voice != "alloy" || request.Audio.ResponseFormat != "mp3" || request.Audio.Instructions != "calm" || request.Audio.Speed == nil || *request.Audio.Speed != 1.25 {
		t.Fatalf("request=%+v audio=%+v", request, request.Audio)
	}
	encoded, err := EncodeAudioSpeech(request)
	if err != nil {
		t.Fatal(err)
	}
	var round map[string]any
	if err := json.Unmarshal(encoded, &round); err != nil {
		t.Fatal(err)
	}
	if round["model"] != "tts-1" || round["input"] != "hello" || round["voice"] != "alloy" || round["response_format"] != "mp3" {
		t.Fatalf("encoded=%s", encoded)
	}
}

func TestDecodeAudioSpeechRejectsMissingFields(t *testing.T) {
	for _, body := range []string{
		`{"model":"tts-1","voice":"alloy"}`,
		`{"model":"tts-1","input":"hi"}`,
		`{"input":"hi","voice":"alloy"}`,
	} {
		if _, err := DecodeAudioSpeech([]byte(body)); err == nil {
			t.Fatalf("expected error for %s", body)
		}
	}
}

func TestDecodeAudioSpeechResponse(t *testing.T) {
	response, err := DecodeAudioSpeechResponse(bytes.NewReader([]byte("RIFF....")), "audio/wav")
	if err != nil {
		t.Fatal(err)
	}
	if response.Audio == nil || string(response.Audio.Data) != "RIFF...." || response.Audio.ContentType != "audio/wav" || string(response.RawContent) != "RIFF...." {
		t.Fatalf("response=%+v", response)
	}
}
