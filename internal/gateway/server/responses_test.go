package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/access/protocol/openai"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

// responsesCore admits and "runs" a request by populating a canned response,
// exercising decode → pipeline plumbing → render for /v1/responses.
type responsesCore struct{}

func (responsesCore) Admit(_ context.Context, input AdmitInput) (*kernel.RequestContext, error) {
	return &kernel.RequestContext{Request: input.Request}, nil
}
func (responsesCore) Run(ctx context.Context, req *kernel.RequestContext, writer contracts.StreamWriter) error {
	if writer != nil {
		chunks := []contracts.StreamChunk{
			{Event: interaction.StreamEvent{Delta: "Hel"}},
			{Event: interaction.StreamEvent{Delta: "lo "}},
			{Event: interaction.StreamEvent{Delta: "stream"}},
			{Event: interaction.StreamEvent{Final: true, StopReason: "stop", Usage: &interaction.UnifiedUsage{InputTokens: 3, OutputTokens: 5}}},
		}
		for _, chunk := range chunks {
			if err := writer.WriteChunk(ctx, chunk); err != nil {
				return err
			}
		}
		return nil
	}
	req.Response = &interaction.UnifiedResponse{
		ID: "resp_test", Model: "gpt-x", CreatedAt: time.Unix(1700000000, 0),
		Usage:   interaction.UnifiedUsage{InputTokens: 3, OutputTokens: 5},
		Choices: []interaction.Choice{{Index: 0, Message: interaction.Message{Role: "assistant", Content: "hello from responses"}}},
	}
	return nil
}
func (responsesCore) Models(context.Context, string, string) (*openai.ModelsResponse, error) {
	return &openai.ModelsResponse{}, nil
}
func (responsesCore) Tool(context.Context, AdmitInput) (*interaction.UnifiedResponse, error) {
	return nil, nil
}

func TestResponsesEndpointRoundTrip(t *testing.T) {
	server := httptest.NewServer(New(responsesCore{}, Config{MaxBodyBytes: 4096}).Handler())
	defer server.Close()
	resp, body := post(t, server.URL+"/v1/responses", `{"model":"gpt-x","input":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	for _, sample := range []string{`"object":"response"`, `"status":"completed"`, `"type":"output_text"`, `"hello from responses"`, `"output_tokens":5`, `"input_tokens":3`} {
		if !strings.Contains(body, sample) {
			t.Errorf("body missing %s:\n%s", sample, body)
		}
	}
}

func TestResponsesStreamingSSE(t *testing.T) {
	server := httptest.NewServer(New(responsesCore{}, Config{MaxBodyBytes: 4096}).Handler())
	defer server.Close()
	resp, body := post(t, server.URL+"/v1/responses", `{"model":"gpt-x","input":"hi","stream":true}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("streaming status=%d body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}
	for _, sample := range []string{
		`"type":"response.created"`, `"type":"response.in_progress"`,
		`"type":"response.output_item.added"`, `"type":"response.content_part.added"`,
		`"type":"response.output_text.delta"`, `"delta":"Hel"`, `"delta":"lo "`, `"delta":"stream"`,
		`"type":"response.output_text.done"`, `"type":"response.content_part.done"`,
		`"type":"response.output_item.done"`, `"type":"response.completed"`,
		`"status":"completed"`, `"output_tokens":5`, `"input_tokens":3`,
	} {
		if !strings.Contains(body, sample) {
			t.Errorf("stream body missing %s:\n%s", sample, body)
		}
	}
	// created must precede the first delta and completed must come last.
	if strings.Index(body, `"response.created"`) > strings.Index(body, `"response.output_text.delta"`) {
		t.Error("response.created must precede the first delta")
	}
	if idx := strings.LastIndex(body, `"response.completed"`); idx < 0 || idx < strings.Index(body, `"response.output_text.done"`) {
		t.Error("response.completed must come after output_text.done")
	}
}

func TestResponsesPlainStringInput(t *testing.T) {
	server := httptest.NewServer(New(responsesCore{}, Config{MaxBodyBytes: 4096}).Handler())
	defer server.Close()
	resp, body := post(t, server.URL+"/v1/responses", `{"model":"gpt-x","input":"hi"}`)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"hello from responses"`) {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}
