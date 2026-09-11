package server

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/F31/liteAIG/internal/access/protocol/openai"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	kernelerrors "github.com/F31/liteAIG/internal/kernel/errors"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/platform/webkit"
)

// fakeCore records the federated transport facts (if any) seen by the last
// Tool call so tests can assert the server forwards trusted-proxy headers.
type fakeCore struct {
	lastFederated *FederatedTransport
}

func (f *fakeCore) Admit(context.Context, AdmitInput) (*kernel.RequestContext, error) {
	return nil, nil
}
func (f *fakeCore) Run(context.Context, *kernel.RequestContext, contracts.StreamWriter) error {
	return nil
}
func (f *fakeCore) Models(context.Context, string, string) (*openai.ModelsResponse, error) {
	return &openai.ModelsResponse{}, nil
}
func (f *fakeCore) Tool(_ context.Context, input AdmitInput) (*interaction.UnifiedResponse, error) {
	f.lastFederated = input.Federated
	if input.Protocol == "a2a" {
		return nil, &kernelerrors.Error{Code: "NOT_FOUND", Message: "A2A agent endpoint is not configured"}
	}
	if input.Request.Tool.Name == "server/discover" {
		return &interaction.UnifiedResponse{ToolResult: &interaction.ToolResult{Content: `{"tools":[{"name":"invoice.read"}]}`}}, nil
	}
	if input.Request.Tool.Name == "tasks/create" {
		return &interaction.UnifiedResponse{ToolResult: &interaction.ToolResult{Content: `{"taskId":"task-1","status":"created"}`}}, nil
	}
	return &interaction.UnifiedResponse{ToolResult: &interaction.ToolResult{Content: "tool-ok"}}, nil
}

func TestMCPAndA2AProtocolRoutes(t *testing.T) {
	server := httptest.NewServer(New(&fakeCore{}, Config{MaxBodyBytes: 1024}).Handler())
	defer server.Close()

	resp, body := post(t, server.URL+"/mcp", `{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"invoice.read","arguments":{"id":"42"}}}`)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"content":"tool-ok"`) {
		t.Fatalf("MCP call status=%d body=%s", resp.StatusCode, body)
	}

	resp, body = post(t, server.URL+"/mcp", `{"jsonrpc":"2.0","id":"2","method":"server/discover","params":{}}`)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"tools"`) || !strings.Contains(body, `invoice.read`) {
		t.Fatalf("MCP discover status=%d body=%s", resp.StatusCode, body)
	}

	resp, body = post(t, server.URL+"/mcp", `{"jsonrpc":"2.0","id":"task","method":"tasks/create","params":{"title":"ship"}}`)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"taskId":"task-1"`) || strings.Contains(body, `"content"`) {
		t.Fatalf("MCP task status=%d body=%s", resp.StatusCode, body)
	}

	resp, body = post(t, server.URL+"/a2a", `{"jsonrpc":"2.0","id":"3","method":"message/send","params":{"message":{"messageId":"m","role":"user","parts":[{"kind":"text","text":"hello"}]}}}`)
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, `A2A agent endpoint is not configured`) {
		t.Fatalf("A2A status=%d body=%s", resp.StatusCode, body)
	}
}

type rerankCore struct{}

func (rerankCore) Admit(_ context.Context, input AdmitInput) (*kernel.RequestContext, error) {
	return &kernel.RequestContext{Request: input.Request}, nil
}
func (rerankCore) Run(_ context.Context, req *kernel.RequestContext, _ contracts.StreamWriter) error {
	req.Response = &interaction.UnifiedResponse{Model: req.Request.Model, Rerank: []interaction.RerankResult{{Index: 1, RelevanceScore: 0.7, Document: "Berlin"}}}
	return nil
}
func (rerankCore) Models(context.Context, string, string) (*openai.ModelsResponse, error) {
	return &openai.ModelsResponse{}, nil
}
func (rerankCore) Tool(context.Context, AdmitInput) (*interaction.UnifiedResponse, error) {
	return nil, nil
}

func TestRerankRoute(t *testing.T) {
	server := httptest.NewServer(New(rerankCore{}, Config{MaxBodyBytes: 1024}).Handler())
	defer server.Close()
	resp, body := post(t, server.URL+"/v1/rerank", `{"model":"rerank-model","query":"capital","documents":["Paris","Berlin"],"top_n":1}`)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"relevance_score":0.7`) || !strings.Contains(body, `"document":"Berlin"`) {
		t.Fatalf("rerank status=%d body=%s", resp.StatusCode, body)
	}
}

func TestAudioTranscriptionRoute(t *testing.T) {
	server := httptest.NewServer(New(audioCore{}, Config{MaxBodyBytes: 1024}).Handler())
	defer server.Close()
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
	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/audio/transcriptions", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(data) != `{"text":"hello audio"}` {
		t.Fatalf("status=%d body=%s", resp.StatusCode, data)
	}
}

func TestBatchCreateRoute(t *testing.T) {
	store := &batchMappingStore{models: map[string]string{}, fileModels: map[string]string{}}
	server := httptest.NewServer(New(batchCore{}, Config{MaxBodyBytes: 1024, BatchMappings: store}).Handler())
	defer server.Close()
	resp, body := post(t, server.URL+"/v1/batches", `{"model":"batch-logical","input_file_id":"file_1","endpoint":"/v1/chat/completions","completion_window":"24h"}`)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"id":"batch_1"`) || !strings.Contains(body, `"status":"validating"`) {
		t.Fatalf("batch status=%d body=%s", resp.StatusCode, body)
	}
	if store.models["batch_1"] != "batch-logical" {
		t.Fatalf("batch mapping = %q", store.models["batch_1"])
	}
	if store.fileModels["file_out"] != "batch-logical" || store.fileModels["file_err"] != "batch-logical" {
		t.Fatalf("batch file mappings = %+v", store.fileModels)
	}
}

func TestBatchLifecycleRoutesUseMapping(t *testing.T) {
	store := &batchMappingStore{models: map[string]string{"batch_1": "batch-logical"}, fileModels: map[string]string{}}
	server := httptest.NewServer(New(batchCore{}, Config{MaxBodyBytes: 1024, BatchMappings: store}).Handler())
	defer server.Close()

	resp, body := get(t, server.URL+"/v1/batches/batch_1")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"status":"in_progress"`) {
		t.Fatalf("retrieve status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = post(t, server.URL+"/v1/batches/batch_1/cancel", ``)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"status":"cancelling"`) {
		t.Fatalf("cancel status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = get(t, server.URL+"/v1/batches?model=batch-logical")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"object":"list"`) || !strings.Contains(body, `"batch_1"`) {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, body)
	}
	if store.fileModels["file_out"] != "batch-logical" {
		t.Fatalf("retrieve did not persist output file mapping: %+v", store.fileModels)
	}
}

func TestBatchFileContentUsesMapping(t *testing.T) {
	store := &batchMappingStore{models: map[string]string{}, fileModels: map[string]string{"file_out": "batch-logical"}}
	server := httptest.NewServer(New(batchCore{}, Config{MaxBodyBytes: 1024, BatchMappings: store}).Handler())
	defer server.Close()

	resp, body := get(t, server.URL+"/v1/files/file_out/content")
	if resp.StatusCode != http.StatusOK || body != "batch-result\n" {
		t.Fatalf("file content status=%d body=%q", resp.StatusCode, body)
	}
	resp, body = get(t, server.URL+"/v1/files/unknown/content")
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "NOT_FOUND") {
		t.Fatalf("unknown file status=%d body=%s", resp.StatusCode, body)
	}
}

func TestFileLifecycleRoutesUseMapping(t *testing.T) {
	store := &batchMappingStore{models: map[string]string{}, fileModels: map[string]string{}}
	server := httptest.NewServer(New(batchCore{}, Config{MaxBodyBytes: 1024, BatchMappings: store}).Handler())
	defer server.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", "batch-logical")
	_ = writer.WriteField("purpose", "batch")
	part, err := writer.CreateFormFile("file", "input.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("{}\n"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/files", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	uploadBody, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(uploadBody), `"id":"file_upload"`) || store.fileModels["file_upload"] != "batch-logical" {
		t.Fatalf("upload status=%d body=%s mappings=%+v", response.StatusCode, uploadBody, store.fileModels)
	}

	resp, text := get(t, server.URL+"/v1/files/file_upload")
	if resp.StatusCode != http.StatusOK || !strings.Contains(text, `"filename":"input.jsonl"`) {
		t.Fatalf("retrieve status=%d body=%s", resp.StatusCode, text)
	}
	resp, text = get(t, server.URL+"/v1/files?model=batch-logical")
	if resp.StatusCode != http.StatusOK || !strings.Contains(text, `"object":"list"`) {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, text)
	}
	resp, text = requestWithMethod(t, http.MethodDelete, server.URL+"/v1/files/file_upload")
	if resp.StatusCode != http.StatusOK || !strings.Contains(text, `"deleted":true`) {
		t.Fatalf("delete status=%d body=%s", resp.StatusCode, text)
	}
	if _, ok := store.fileModels["file_upload"]; ok {
		t.Fatalf("delete did not revoke file mapping: %+v", store.fileModels)
	}
	resp, text = get(t, server.URL+"/v1/files/file_upload")
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(text, "NOT_FOUND") {
		t.Fatalf("deleted file retrieve status=%d body=%s", resp.StatusCode, text)
	}
	resp, text = get(t, server.URL+"/v1/files")
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(text, "INVALID_REQUEST") {
		t.Fatalf("list without model status=%d body=%s", resp.StatusCode, text)
	}
}

func TestBatchListRequiresModel(t *testing.T) {
	server := httptest.NewServer(New(batchCore{}, Config{MaxBodyBytes: 1024}).Handler())
	defer server.Close()
	resp, body := get(t, server.URL+"/v1/batches")
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "INVALID_REQUEST") {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, body)
	}
}

type batchMappingStore struct {
	models     map[string]string
	fileModels map[string]string
}

func (s *batchMappingStore) SaveBatchMapping(_ context.Context, id, model string) error {
	s.models[id] = model
	return nil
}

func (s *batchMappingStore) ModelForBatch(_ context.Context, id string) (string, error) {
	model, ok := s.models[id]
	if !ok {
		return "", errNotFoundForTest{}
	}
	return model, nil
}

func (s *batchMappingStore) SaveBatchFileMapping(_ context.Context, _, id, model string) error {
	return s.SaveFileMapping(context.Background(), id, model, "batch_result", "batch")
}

func (s *batchMappingStore) ModelForBatchFile(_ context.Context, id string) (string, error) {
	return s.ModelForFile(context.Background(), id)
}

func (s *batchMappingStore) SaveFileMapping(_ context.Context, id, model, _, _ string) error {
	if id != "" {
		s.fileModels[id] = model
	}
	return nil
}

func (s *batchMappingStore) ModelForFile(_ context.Context, id string) (string, error) {
	model, ok := s.fileModels[id]
	if !ok {
		return "", errNotFoundForTest{}
	}
	return model, nil
}

func (s *batchMappingStore) DeleteFileMapping(_ context.Context, id string) error {
	delete(s.fileModels, id)
	return nil
}

type errNotFoundForTest struct{}

func (errNotFoundForTest) Error() string { return "not found" }

type batchCore struct{}

func (batchCore) Admit(_ context.Context, input AdmitInput) (*kernel.RequestContext, error) {
	return &kernel.RequestContext{Request: input.Request}, nil
}
func (batchCore) Run(_ context.Context, req *kernel.RequestContext, _ contracts.StreamWriter) error {
	if req.Request.Kind != interaction.RequestBatch && req.Request.Kind != interaction.RequestFile {
		return &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "bad batch"}
	}
	if req.Request.Kind == interaction.RequestFile {
		if req.Request.File.Operation == "upload" {
			if string(req.Request.File.Data) != "{}\n" || req.Request.File.Purpose != "batch" {
				return &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "bad upload"}
			}
			req.Response = &interaction.UnifiedResponse{File: &interaction.FileResult{ID: "file_upload", Object: "file", Filename: req.Request.File.Filename, Purpose: req.Request.File.Purpose}}
			return nil
		}
		if req.Request.File.Operation == "list" {
			req.Response = &interaction.UnifiedResponse{File: &interaction.FileResult{Object: "list", Items: []interaction.FileResult{{ID: "file_upload", Object: "file", Filename: "input.jsonl", Purpose: "batch"}}}}
			return nil
		}
		if req.Request.File.Operation == "retrieve" {
			req.Response = &interaction.UnifiedResponse{File: &interaction.FileResult{ID: req.Request.File.ID, Object: "file", Filename: "input.jsonl", Purpose: "batch"}}
			return nil
		}
		if req.Request.File.Operation == "delete" {
			req.Response = &interaction.UnifiedResponse{File: &interaction.FileResult{ID: req.Request.File.ID, Object: "file", Deleted: true}}
			return nil
		}
		if req.Request.File.ID != "file_out" {
			return &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "bad file"}
		}
		req.Response = &interaction.UnifiedResponse{RawContent: []byte("batch-result\n")}
		return nil
	}
	switch req.Request.Batch.Operation {
	case "", "create":
		if req.Request.Batch.InputFileID != "file_1" {
			return &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "bad batch"}
		}
		req.Response = &interaction.UnifiedResponse{Batch: &interaction.BatchResult{ID: "batch_1", Object: "batch", Endpoint: req.Request.Batch.Endpoint, InputFileID: req.Request.Batch.InputFileID, OutputFileID: "file_out", ErrorFileID: "file_err", Status: "validating", CompletionWindow: req.Request.Batch.CompletionWindow}}
	case "retrieve":
		req.Response = &interaction.UnifiedResponse{Batch: &interaction.BatchResult{ID: req.Request.Batch.ID, Object: "batch", OutputFileID: "file_out", Status: "in_progress"}}
	case "cancel":
		req.Response = &interaction.UnifiedResponse{Batch: &interaction.BatchResult{ID: req.Request.Batch.ID, Object: "batch", Status: "cancelling"}}
	case "list":
		req.Response = &interaction.UnifiedResponse{Batch: &interaction.BatchResult{Object: "list", Items: []interaction.BatchResult{{ID: "batch_1", Object: "batch", Status: "completed"}}}}
	}
	return nil
}
func (batchCore) Models(context.Context, string, string) (*openai.ModelsResponse, error) {
	return &openai.ModelsResponse{}, nil
}
func (batchCore) Tool(context.Context, AdmitInput) (*interaction.UnifiedResponse, error) {
	return nil, nil
}

type audioCore struct{}

func (audioCore) Admit(_ context.Context, input AdmitInput) (*kernel.RequestContext, error) {
	return &kernel.RequestContext{Request: input.Request}, nil
}
func (audioCore) Run(_ context.Context, req *kernel.RequestContext, _ contracts.StreamWriter) error {
	if req.Request.Kind != interaction.RequestAudio || string(req.Request.Audio.Data) != "audio-bytes" {
		return &kernelerrors.Error{Code: "INVALID_REQUEST", Message: "bad audio"}
	}
	req.Response = &interaction.UnifiedResponse{Audio: &interaction.AudioResult{Text: "hello audio"}}
	return nil
}
func (audioCore) Models(context.Context, string, string) (*openai.ModelsResponse, error) {
	return &openai.ModelsResponse{}, nil
}
func (audioCore) Tool(context.Context, AdmitInput) (*interaction.UnifiedResponse, error) {
	return nil, nil
}

func TestBearerTokenAcceptsAnthropicAPIKeyHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Header.Set("X-Api-Key", "sk-lia-v1_test")
	if got := bearerToken(req); got != "sk-lia-v1_test" {
		t.Fatalf("bearerToken() = %q", got)
	}
}

func TestGatewayAPICompatibilityHeaders(t *testing.T) {
	server := httptest.NewServer(New(&fakeCore{}, Config{MaxBodyBytes: 1024}).Handler())
	defer server.Close()

	resp, body := getWithHeaders(t, server.URL+"/v1/models", nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get(webkit.APIContractHeader) != webkit.CurrentAPIContract {
		t.Fatalf("current status=%d header=%q body=%s", resp.StatusCode, resp.Header.Get(webkit.APIContractHeader), body)
	}

	resp, body = getWithHeaders(t, server.URL+"/v1/models", map[string]string{webkit.APIContractHeader: "1999-01-01"})
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "UNSUPPORTED_API_VERSION") {
		t.Fatalf("unsupported status=%d body=%s", resp.StatusCode, body)
	}

	resp, _ = getWithHeaders(t, server.URL+"/v1/models", map[string]string{webkit.APIContractHeader: webkit.DeprecatedAPIContract})
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Deprecation") != "true" || resp.Header.Get("Sunset") != webkit.DeprecatedAPIContractEnd {
		t.Fatalf("deprecated status=%d headers=%v", resp.StatusCode, resp.Header)
	}
}

func getWithHeaders(t *testing.T, url string, headers map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

func requestWithMethod(t *testing.T, method, url string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

func post(t *testing.T, url, body string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(buf)
}

func TestA2AJSONRPCDispatchValidation(t *testing.T) {
	server := httptest.NewServer(New(&fakeCore{}, Config{MaxBodyBytes: 2048}).Handler())
	defer server.Close()
	cases := []struct {
		name   string
		body   string
		status int
		want   string
	}{
		{name: "message_send_valid", body: `{"jsonrpc":"2.0","id":"1","method":"message/send","params":{"message":{"messageId":"m","role":"user","parts":[{"kind":"text","text":"hello"}]}}}`, status: http.StatusNotFound, want: "A2A agent endpoint is not configured"},
		{name: "unsupported_method_rejected", body: `{"jsonrpc":"2.0","id":"2","method":"tasks/cancel","params":{}}`, status: http.StatusNotFound, want: `-32601`},
		{name: "wrong_version_rejected", body: `{"jsonrpc":"1.0","id":"3","method":"message/send","params":{"message":{"messageId":"m","role":"user","parts":[{"kind":"text","text":"hi"}]}}}`, status: http.StatusBadRequest, want: `-32600`},
		{name: "missing_message_rejected", body: `{"jsonrpc":"2.0","id":"4","method":"message/send","params":{}}`, status: http.StatusBadRequest, want: `-32602`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := post(t, server.URL+"/a2a", tc.body)
			if resp.StatusCode != tc.status {
				t.Fatalf("status=%d body=%s", resp.StatusCode, body)
			}
			if !strings.Contains(body, tc.want) {
				t.Fatalf("body missing %q:\n%s", tc.want, body)
			}
		})
	}
}

// postWithHeaders performs a POST with explicit headers, returning the raw
// response so callers can assert on transport facts delivered to the core.
func postWithHeaders(t *testing.T, url, body string, headers map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(buf)
}

func TestA2AFederatedHeadersPassToCoreWithNoBearer(t *testing.T) {
	core := &fakeCore{}
	server := httptest.NewServer(New(core, Config{MaxBodyBytes: 2048}).Handler())
	defer server.Close()

	body := `{"jsonrpc":"2.0","id":"1","method":"message/send","params":{"message":{"messageId":"m","role":"user","parts":[{"kind":"text","text":"hello"}]}}}`
	_, _ = postWithHeaders(t, server.URL+"/a2a", body, map[string]string{
		"X-A2A-Auth-Method": "mtls_spki",
		"X-A2A-Subject":     "spki:partner-cert",
		"X-A2A-Issuer":      "registry.example",
	})
	if core.lastFederated == nil {
		t.Fatal("core did not receive federated transport facts")
	}
	if core.lastFederated.AuthMethod != "mtls_spki" || core.lastFederated.Subject != "spki:partner-cert" || core.lastFederated.Issuer != "registry.example" {
		t.Fatalf("federated facts = %+v", core.lastFederated)
	}
}

func TestA2ABearerSuppressesFederatedHeaders(t *testing.T) {
	core := &fakeCore{}
	server := httptest.NewServer(New(core, Config{MaxBodyBytes: 2048}).Handler())
	defer server.Close()

	body := `{"messageId":"m","role":"user","parts":[{"kind":"text","text":"hello"}]}`
	_, _ = postWithHeaders(t, server.URL+"/a2a", body, map[string]string{
		"Authorization":     "Bearer sk-lia-v1_test",
		"X-Api-Key":         "sk-other",
		"X-A2A-Auth-Method": "mtls_spki",
		"X-A2A-Subject":     "spki:partner-cert",
	})
	if core.lastFederated != nil {
		t.Fatalf("bearer-authenticated request leaked federated facts: %+v", core.lastFederated)
	}
}

func TestA2AIncompleteFederatedHeadersIgnored(t *testing.T) {
	core := &fakeCore{}
	server := httptest.NewServer(New(core, Config{MaxBodyBytes: 2048}).Handler())
	defer server.Close()

	body := `{"messageId":"m","role":"user","parts":[{"kind":"text","text":"hello"}]}`
	// Subject without a method (and vice versa) must not produce facts.
	_, _ = postWithHeaders(t, server.URL+"/a2a", body, map[string]string{
		"X-A2A-Subject": "spki:partner-cert",
	})
	if core.lastFederated != nil {
		t.Fatalf("method-less headers produced facts: %+v", core.lastFederated)
	}
	core.lastFederated = nil
	_, _ = postWithHeaders(t, server.URL+"/a2a", body, map[string]string{
		"X-A2A-Auth-Method": "mtls_spki",
	})
	if core.lastFederated != nil {
		t.Fatalf("subject-less headers produced facts: %+v", core.lastFederated)
	}
}
