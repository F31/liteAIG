package openai

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

func TestDecodeEncodeRerank(t *testing.T) {
	topN := 1
	request, err := DecodeRerankBytes([]byte(`{"model":"rerank-model","query":"capital","documents":["Paris","Berlin"],"top_n":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.Kind != interaction.RequestRerank || request.Rerank.Query != "capital" || *request.Rerank.TopN != topN || len(request.Rerank.Documents) != 2 {
		t.Fatalf("request=%+v", request)
	}
	encoded, err := EncodeRerank(request)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["model"] != "rerank-model" || wire["query"] != "capital" || wire["top_n"] != float64(1) {
		t.Fatalf("wire=%v", wire)
	}
}

func TestDecodeEncodeRerankResponse(t *testing.T) {
	response, err := DecodeRerankResponse(bytes.NewReader([]byte(`{"model":"rerank-model","results":[{"index":1,"relevance_score":0.9,"document":{"text":"Berlin"}}],"usage":{"prompt_tokens":3,"total_tokens":3}}`)))
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Rerank) != 1 || response.Rerank[0].Index != 1 || response.Rerank[0].Document != "Berlin" || response.Usage.TotalTokens() != 3 {
		t.Fatalf("response=%+v", response)
	}
	encoded, err := EncodeRerankResponse(response, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"relevance_score":0.9`)) || !bytes.Contains(encoded, []byte(`"document":"Berlin"`)) {
		t.Fatalf("encoded=%s", encoded)
	}
}
