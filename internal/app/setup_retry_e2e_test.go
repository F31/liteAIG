package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetupAuthFailureIsVisibleThenRetryable proves the wizard no longer
// deadlocks an install on a bad provider credential:
//   - a rejected credential surfaces a specific error code (not a generic 500);
//   - the failed run leaves only a half-initialized state;
//   - retrying after fixing the credential clears that half-state and completes,
//     so first-run setup is recoverable from the Console.
func TestSetupAuthFailureIsVisibleThenRetryable(t *testing.T) {
	provider := newTestProviderServer(t)
	dsn := "file:" + filepath.Join(t.TempDir(), "retryable.db")
	lite, err := NewLite(context.Background(), LiteOptions{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	admin := httptest.NewServer(lite.Handler())
	t.Cleanup(func() {
		admin.Close()
		_ = lite.Close()
	})

	setup := func(secret string) (int, string) {
		body := map[string]string{
			"username": "admin", "adminPassword": "password-123456", "tenantName": "Acme",
			"providerName": "Test Provider", "providerType": "openai",
			"providerEndpoint": providerEndpoint(provider), "providerSecret": secret,
			"selectedModel": "gpt-4o-mini",
		}
		return doJSON(t, admin.URL+"/api/admin/setup", http.MethodPost, body, "", "")
	}

	// A wrong credential is rejected with a specific, user-visible code.
	status, body := setup("wrong-key")
	if status != http.StatusBadRequest || !strings.Contains(body, "SETUP_PROVIDER_AUTH_FAILED") {
		t.Fatalf("bad credential setup status = %d body = %s, want 400 SETUP_PROVIDER_AUTH_FAILED", status, body)
	}

	// The corrected credential completes on retry: the interrupted attempt is
	// rolled back instead of returning ALREADY_INITIALIZED.
	status, body = setup(testProviderKey)
	if status != http.StatusOK {
		t.Fatalf("retry setup status = %d body = %s, want 200", status, body)
	}
	var result struct {
		VirtualKey string `json:"virtualKey"`
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil || result.VirtualKey == "" {
		t.Fatalf("retry result body = %s, want a virtual key", body)
	}
}
