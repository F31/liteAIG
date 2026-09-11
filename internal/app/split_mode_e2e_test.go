package app

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/platform/bundle"
)

// splitControl boots a Control Plane with a bundle signer + shared token and
// runs the setup wizard to publish a tenant config, which signs a RuntimeBundle.
// The caller owns closing lite and the returned admin server.
func splitControl(t *testing.T, dir string, token string, signer *bundle.Signer) (*Lite, *httptest.Server) {
	t.Helper()
	lite, err := NewLite(context.Background(), LiteOptions{
		DSN:          "file:" + filepath.Join(dir, "control.db"),
		BundleSigner: signer,
		BundleToken:  token,
	})
	if err != nil {
		t.Fatal(err)
	}
	admin := httptest.NewServer(lite.Handler())
	providerURL := providerEndpoint(newTestProviderServer(t))
	setupBody := map[string]string{
		"username": "admin", "adminPassword": "password-123456", "tenantName": "Acme",
		"providerName": "Test Provider", "providerType": "openai",
		"providerEndpoint": providerURL, "providerSecret": testProviderKey,
		"selectedModel": "gpt-4o-mini",
	}
	status, body := doJSON(t, admin.URL+"/api/admin/setup", "POST", setupBody, "", "")
	if status != http.StatusOK {
		t.Fatalf("setup status = %d body = %s", status, body)
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
	// Publish the seeded config so a signed bundle is generated.
	status, body = doJSON(t, admin.URL+"/api/admin/config/drafts", "POST", nil, cookies, session.CSRFToken)
	if status != http.StatusOK {
		t.Fatalf("create draft status = %d body = %s", status, body)
	}
	var draft struct {
		ID       string `json:"ID"`
		Revision int64  `json:"Revision"`
	}
	if err := json.Unmarshal([]byte(body), &draft); err != nil {
		t.Fatal(err)
	}
	status, body = doJSON(t, admin.URL+"/api/admin/config/drafts/"+draft.ID+"/publish", "POST",
		map[string]int64{"revision": draft.Revision}, cookies, session.CSRFToken)
	if status != http.StatusOK {
		t.Fatalf("publish status = %d body = %s", status, body)
	}
	return lite, admin
}

// splitGateway boots a mode=gateway process that pulls signed bundles from the
// control URL instead of relying on its own (empty) management store. Each
// gateway gets its own LKG directory so it never boots from the Control Plane's
// LKG bundle by structural fallback.
func splitGateway(t *testing.T, dir, name, controlURL, token, publicKeyHex string) (*Lite, *httptest.Server) {
	t.Helper()
	gatewayDir := filepath.Join(dir, name)
	if err := os.MkdirAll(gatewayDir, 0o750); err != nil {
		t.Fatal(err)
	}
	lite, err := NewLite(context.Background(), LiteOptions{
		DSN:                "file:" + filepath.Join(gatewayDir, "gw.db"),
		GatewayAddr:        "127.0.0.1:0",
		ControlURL:         controlURL,
		BundleToken:        token,
		BundlePublicKeyHex: publicKeyHex,
	})
	if err != nil {
		t.Fatal(err)
	}
	return lite, httptest.NewServer(lite.GatewayHandler())
}

// waitForRuntime polls until the gateway activates a tenant from the pulled
// signed bundle (the syncer runs its cold-start fetch on a background ticker).
func waitForRuntime(t *testing.T, lite *Lite, label string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if lite.HasActiveRuntime() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never activated a runtime from the Control Plane", label)
}

// TestSplitModeGatewaysActivateAndSurviveControlDown is the split-mode e2e:
// two gateways pull a signed bundle from a Control Plane, both activate it,
// and after the Control Plane dies they keep serving from their loaded runtime.
func TestSplitModeGatewaysActivateAndSurviveControlDown(t *testing.T) {
	dir := t.TempDir()
	token := "control-secret"

	_, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer := bundle.NewSigner(privateKey, "v1", 1)
	control, adminSrv := splitControl(t, dir, token, signer)
	defer control.Close()
	defer adminSrv.Close()

	// Two gateways pull from the control admin URL.
	pubHex := signer.PublicKeyHex()
	gatewayA, gwA := splitGateway(t, dir, "gw-a", adminSrv.URL, token, pubHex)
	defer gatewayA.Close()
	defer gwA.Close()
	gatewayB, gwB := splitGateway(t, dir, "gw-b", adminSrv.URL, token, pubHex)
	defer gatewayB.Close()
	defer gwB.Close()

	waitForRuntime(t, gatewayA, "gateway A")
	waitForRuntime(t, gatewayB, "gateway B")

	// Kill the Control Plane: shut down the admin listener the gateways pull
	// from, then the Lite itself. Gateways keep serving from loaded runtimes.
	adminSrv.Close()
	_ = control.Close()

	if !gatewayA.HasActiveRuntime() || !gatewayB.HasActiveRuntime() {
		t.Fatal("a gateway lost its runtime after the Control Plane died")
	}
	checkGatewayServing(t, gwA, "gateway A")
	checkGatewayServing(t, gwB, "gateway B")
}

// checkGatewayServing verifies the data plane answers after the Control Plane
// went away. /v1/models exercises the loaded tenant runtime without a key.
func checkGatewayServing(t *testing.T, server *httptest.Server, label string) {
	t.Helper()
	resp, err := http.Get(server.URL + "/v1/models")
	if err != nil {
		t.Fatalf("%s after control-down: %v", label, err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusNotFound {
		t.Fatalf("%s not serving after control-down: %d", label, resp.StatusCode)
	}
}

// TestSplitModeBadSignatureNACKKeepsCurrent verifies that a Control Plane whose
// bundle signature does not verify (wrong public key on the gateway) is NACKed:
// the gateway stays without a runtime instead of loading a tampered bundle.
func TestSplitModeBadSignatureNACKKeepsCurrent(t *testing.T) {
	dir := t.TempDir()
	token := "control-secret"

	_, privateKey, _ := ed25519.GenerateKey(nil)
	signer := bundle.NewSigner(privateKey, "v1", 1)
	control, adminSrv := splitControl(t, dir, token, signer)
	defer control.Close()
	defer adminSrv.Close()

	// A different key: the gateway must NACK the signed bundle.
	wrongPublic, _, _ := ed25519.GenerateKey(nil)
	gateway, gwSrv := splitGateway(t, dir, "gw-wrong", adminSrv.URL, token, pubHexOf(wrongPublic))
	defer gateway.Close()
	defer gwSrv.Close()

	// Give the syncer time to run; it must never activate the bundle.
	time.Sleep(500 * time.Millisecond)
	if gateway.HasActiveRuntime() {
		t.Fatal("gateway loaded a bundle whose signature did not verify")
	}
}

func pubHexOf(public ed25519.PublicKey) string {
	return hexEncode(public)
}

func hexEncode(data []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(data)*2)
	for _, b := range data {
		out = append(out, digits[b>>4], digits[b&0xf])
	}
	return string(out)
}
