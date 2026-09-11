package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestLKGBootFallback proves the Last Known Good wiring: a successful publish
// persists an LKG bundle, and a later boot whose Control Plane has no active
// runtime for that tenant falls back to the persisted bundle.
func TestLKGBootFallback(t *testing.T) {
	dir := t.TempDir()
	providerURL := providerEndpoint(newTestProviderServer(t))

	setup := func(dsn string) *Lite {
		lite, err := NewLite(context.Background(), LiteOptions{DSN: dsn})
		if err != nil {
			t.Fatal(err)
		}
		admin := httptest.NewServer(lite.Handler())
		t.Cleanup(admin.Close)
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
		// Publish a config so the LKG bundle is persisted.
		status, body = doJSON(t, admin.URL+"/api/admin/config/drafts", "POST", nil, cookies, session.CSRFToken)
		if status != http.StatusOK {
			t.Fatalf("create draft status = %d body = %s", status, body)
		}
		var draft struct {
			ID       string          `json:"ID"`
			Revision int64           `json:"Revision"`
			Config   json.RawMessage `json:"Config"`
		}
		if err := json.Unmarshal([]byte(body), &draft); err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := json.Unmarshal(draft.Config, &document); err != nil {
			t.Fatal(err)
		}
		document["cache"] = map[string]any{"enabled": true, "ttl_seconds": 60, "namespace_version": 1}
		updated, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		status, body = doJSON(t, admin.URL+"/api/admin/config/drafts/"+draft.ID, "PUT", map[string]any{"revision": draft.Revision, "config": json.RawMessage(updated)}, cookies, session.CSRFToken)
		if status != http.StatusOK {
			t.Fatalf("update draft status = %d body = %s", status, body)
		}
		var updatedDraft struct {
			Revision int64 `json:"Revision"`
		}
		if err := json.Unmarshal([]byte(body), &updatedDraft); err != nil {
			t.Fatal(err)
		}
		status, body = doJSON(t, admin.URL+"/api/admin/config/drafts/"+draft.ID+"/publish", "POST", map[string]int64{"revision": updatedDraft.Revision}, cookies, session.CSRFToken)
		if status != http.StatusOK {
			t.Fatalf("publish status = %d body = %s", status, body)
		}
		return lite
	}

	// Instance A: publish a config; the LKG bundle must be persisted to disk.
	liteA := setup("file:" + filepath.Join(dir, "a.db"))
	if err := liteA.Close(); err != nil {
		t.Fatal(err)
	}
	lkgDir := filepath.Join(dir, "liteaig-lkg")
	refs, err := lkgTenantRefs(lkgDir)
	if err != nil || len(refs) == 0 {
		t.Fatalf("no LKG bundle persisted (refs=%v err=%v), want at least one", refs, err)
	}
	activeBundle := filepath.Join(lkgDir, refs[0], "active.bundle")
	if _, err := os.Stat(activeBundle); err != nil {
		t.Fatalf("active LKG bundle missing at %s: %v", activeBundle, err)
	}

	// Instance B: an empty Control Plane (different DB, same LKG dir) must boot
	// the tenant from the Last Known Good bundle.
	liteB := setupEmpty("file:" + filepath.Join(dir, "b.db"))
	defer liteB.Close()
	if !liteB.HasActiveRuntime() {
		t.Fatal("instance B has no active runtime, want the tenant booted from the LKG bundle")
	}
}

// setupEmpty boots a Lite instance without running setup (empty Control Plane)
// so no tenant is activated from the database, exercising the LKG fallback.
func setupEmpty(dsn string) *Lite {
	lite, err := NewLite(context.Background(), LiteOptions{DSN: dsn})
	if err != nil {
		panic(err)
	}
	return lite
}

// lkgTenantRefs lists the tenant subdirectories under an LKG root.
func lkgTenantRefs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var refs []string
	for _, entry := range entries {
		if entry.IsDir() {
			refs = append(refs, entry.Name())
		}
	}
	return refs, nil
}
