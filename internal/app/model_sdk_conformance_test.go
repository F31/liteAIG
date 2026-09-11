package app

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestOfficialModelSDKInterop is the OpenAI/Anthropic counterpart to the A2A
// conformance hinge: it boots a real Lite gateway and drives it with the
// official Python SDKs, changing only api_key and base_url.
func TestOfficialModelSDKInterop(t *testing.T) {
	if os.Getenv("LITEAIG_MODEL_SDK_CONFORMANCE") == "" {
		t.Skip("LITEAIG_MODEL_SDK_CONFORMANCE not set; official model SDK interop runs on the conformance runner")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available on this runner")
	}
	openaiDriver := conformanceDriver(t, "model", "openai_client.py")
	anthropicDriver := conformanceDriver(t, "model", "anthropic_client.py")

	gateway, key := bootGatewayWithTestProvider(t)
	started := time.Now()
	openai := runModelSDKDriver(t, python, openaiDriver, "--base-url", gateway.URL+"/v1", "--token", key, "--model", "default-chat")
	anthropic := runModelSDKDriver(t, python, anthropicDriver, "--base-url", gateway.URL, "--token", key, "--model", "default-chat")

	if !strings.Contains(string(openai.Result), "provider-echo") || !strings.Contains(string(openai.Result), "stream-ok") {
		t.Fatalf("official OpenAI SDK result missing blocking/streaming echo: %s", openai.Result)
	}
	if !strings.Contains(string(openai.Result), "default-chat") {
		t.Fatalf("official OpenAI SDK result missing models list: %s", openai.Result)
	}
	if !strings.Contains(string(anthropic.Result), "provider-echo") {
		t.Fatalf("official Anthropic SDK result missing relay echo: %s", anthropic.Result)
	}

	report := map[string]any{"test": "TestOfficialModelSDKInterop", "ok": true, "url": gateway.URL, "duration": time.Since(started).String(), "openai": openai, "anthropic": anthropic}
	reportPath := os.Getenv("LITEAIG_MODEL_SDK_REPORT")
	if reportPath == "" {
		reportPath = "/tmp/model-sdk-interop-report.json"
	}
	if data, err := json.MarshalIndent(report, "", "  "); err == nil {
		_ = os.WriteFile(reportPath, data, 0o600)
	}
}

type modelSDKReport struct {
	Sent   bool            `json:"sent"`
	Result json.RawMessage `json:"-"`
	Error  string          `json:"error"`
}

func runModelSDKDriver(t *testing.T, python, driver string, args ...string) modelSDKReport {
	t.Helper()
	command := exec.Command(python, append([]string{driver}, args...)...)
	command.Env = os.Environ()
	output, runErr := command.CombinedOutput()
	t.Logf("model sdk driver %s output: %s", filepath.Base(driver), output)
	if runErr != nil {
		t.Fatalf("official model SDK driver failed (%v): %s", runErr, output)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(output, &body); err != nil {
		t.Fatalf("driver report is not JSON: %v\n%s", err, output)
	}
	var sent bool
	_ = json.Unmarshal(body["sent"], &sent)
	if !sent {
		var message string
		_ = json.Unmarshal(body["error"], &message)
		t.Fatalf("official model SDK could not complete interop: %s", message)
	}
	return modelSDKReport{Sent: sent, Result: json.RawMessage(output)}
}

func conformanceDriver(t *testing.T, parts ...string) string {
	t.Helper()
	pathParts := append([]string{"..", "..", "tests", "conformance"}, parts...)
	driver, err := filepath.Abs(filepath.Join(pathParts...))
	if err != nil {
		t.Fatal(err)
	}
	return driver
}

func bootGatewayWithTestProvider(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	provider := newTestProviderServer(t)
	gateway, key := bootGatewayWithLocalA2A(t, provider.URL)
	return gateway, key
}
