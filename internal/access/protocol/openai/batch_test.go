package openai

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

func TestDecodeEncodeBatchCreate(t *testing.T) {
	request, err := DecodeBatchCreateBytes([]byte(`{"model":"batch-logical","input_file_id":"file_1","endpoint":"/v1/chat/completions","completion_window":"24h","metadata":{"job":"nightly"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.Kind != interaction.RequestBatch || request.Model != "batch-logical" || request.Batch.InputFileID != "file_1" || request.Batch.Endpoint != "/v1/chat/completions" {
		t.Fatalf("request=%+v", request)
	}
	encoded, err := EncodeBatchCreate(request)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if _, ok := wire["model"]; ok || wire["input_file_id"] != "file_1" || wire["completion_window"] != "24h" {
		t.Fatalf("wire=%v", wire)
	}
}

func TestDecodeEncodeBatchResponse(t *testing.T) {
	body := []byte(`{"id":"batch_1","object":"batch","endpoint":"/v1/chat/completions","input_file_id":"file_1","output_file_id":"file_out","error_file_id":"file_err","status":"validating","completion_window":"24h"}`)
	response, err := DecodeBatchResponse(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if response.Batch.ID != "batch_1" || response.Batch.OutputFileID != "file_out" || response.Batch.ErrorFileID != "file_err" || response.Batch.Status != "validating" || !bytes.Equal(response.Batch.Raw, body) {
		t.Fatalf("response=%+v", response.Batch)
	}
	encoded, err := EncodeBatchResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, body) {
		t.Fatalf("encoded=%s", encoded)
	}
}

func TestDecodeEncodeBatchListResponse(t *testing.T) {
	body := []byte(`{"object":"list","data":[{"id":"batch_1","object":"batch","status":"completed"}],"has_more":false,"first_id":"batch_1","last_id":"batch_1"}`)
	response, err := DecodeBatchResponse(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if response.Batch.Object != "list" || len(response.Batch.Items) != 1 || response.Batch.Items[0].ID != "batch_1" {
		t.Fatalf("response=%+v", response.Batch)
	}
	encoded, err := EncodeBatchResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, body) {
		t.Fatalf("encoded=%s", encoded)
	}
}
