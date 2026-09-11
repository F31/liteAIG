package openai

import (
	"bytes"
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
	if request.Kind != interaction.RequestAudio || request.Model != "whisper-1" || request.Audio.Filename != "sample.wav" || string(request.Audio.Data) != "audio-bytes" || *request.Audio.Temperature != 0.2 {
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
