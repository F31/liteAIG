package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestA2AOfficialSDKInterop is the interop conformance hinge for the trusted
// release: it boots a real Lite gateway, publishes a local A2A relay, and
// drives the gateway /a2a endpoint with the OFFICIAL A2A SDK client (installed
// via pip `a2a-sdk`, latest; the driver prefers the v1.x ClientFactory /
// SendMessage path and the SendStreamingMessage path) so the outsourced wire
// contract is exercised, not just our own connector. It is env-gated: without
// LITEAIG_A2A_SDK_DIR (and python3) the test skips, so offline CI stays green;
// the conformance workflow
// (.github/workflows/conformance.yaml, runbook docs/A2A_CONFORMANCE.md) sets it
// on a networked runner.
func TestA2AOfficialSDKInterop(t *testing.T) {
	sdkDir := os.Getenv("LITEAIG_A2A_SDK_DIR")
	if sdkDir == "" {
		t.Skip("LITEAIG_A2A_SDK_DIR not set; official A2A SDK interop runs on the conformance runner")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available on this runner")
	}
	sdkPython := os.Getenv("LITEAIG_A2A_SDK_PYTHON")
	if sdkPython == "" {
		sdkPython = filepath.Join(sdkDir, "sdk", "python", "src")
	}
	driver, err := filepath.Abs(filepath.Join("..", "..", "tests", "conformance", "a2a", "client.py"))
	if err != nil {
		t.Fatal(err)
	}

	const echo = "echo-interop"
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":"1","result":{"message":{"parts":[{"kind":"text","text":%q}]}}}`, echo)
	}))
	t.Cleanup(relay.Close)

	gateway, key := bootGatewayWithLocalA2A(t, relay.URL)
	if key == "" {
		t.Fatal("booted gateway has no API key")
	}

	started := time.Now()
	blocking := runA2ASDKDriver(t, python, driver, sdkPython, gateway.URL+"/a2a", key, "hello official sdk", false)
	streaming := runA2ASDKDriver(t, python, driver, sdkPython, gateway.URL+"/a2a", key, "hello official sdk streaming", true)
	elapsed := time.Since(started)
	if !strings.Contains(string(blocking.Result), echo) {
		t.Fatalf("official SDK-parsed blocking result missing relay echo %q: %s", echo, blocking.Result)
	}
	if !strings.Contains(string(blocking.Result), `"message"`) && !strings.Contains(string(blocking.Result), `"task"`) {
		t.Fatalf("official SDK-parsed blocking result missing a message or task payload: %s", blocking.Result)
	}
	if !strings.Contains(string(streaming.Result), echo) {
		t.Fatalf("official SDK-parsed streaming result missing relay echo %q: %s", echo, streaming.Result)
	}
	if !strings.Contains(string(streaming.Result), `"message"`) {
		t.Fatalf("official SDK-parsed streaming result missing a message event: %s", streaming.Result)
	}
	// Persist a machine-readable report for the CI artifact.
	interopReport := map[string]any{
		"test":      "TestA2AOfficialSDKInterop",
		"ok":        true,
		"url":       gateway.URL + "/a2a",
		"echo":      echo,
		"sdk_dir":   sdkDir,
		"sdk_py":    sdkPython,
		"duration":  elapsed.String(),
		"blocking":  blocking,
		"streaming": streaming,
	}
	reportPath := os.Getenv("LITEAIG_A2A_REPORT")
	if reportPath == "" {
		reportPath = "/tmp/interop-report.json"
	}
	if data, err := json.MarshalIndent(interopReport, "", "  "); err == nil {
		_ = os.WriteFile(reportPath, data, 0o600)
	}
}

type a2aSDKReport struct {
	Sent   bool            `json:"sent"`
	Mode   string          `json:"mode"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

func runA2ASDKDriver(t *testing.T, python, driver, sdkPython, url, key, text string, stream bool) a2aSDKReport {
	t.Helper()
	args := []string{driver, "--url", url, "--text", text, "--token", key}
	if stream {
		args = append(args, "--stream")
	}
	command := exec.Command(python, args...)
	command.Env = append(os.Environ(), "LITEAIG_A2A_SDK_PYTHON="+sdkPython)
	output, runErr := command.CombinedOutput()
	t.Logf("sdk driver stream=%t output: %s", stream, output)
	if runErr != nil {
		t.Fatalf("official SDK driver failed (stream=%t, %v): %s", stream, runErr, output)
	}
	var report a2aSDKReport
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("driver report is not JSON (stream=%t): %v\n%s", stream, err, output)
	}
	if !report.Sent {
		t.Fatalf("official SDK could not complete interop (stream=%t): %s", stream, report.Error)
	}
	return report
}
