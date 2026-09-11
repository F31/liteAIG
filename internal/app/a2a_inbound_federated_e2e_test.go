package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/F31/liteAIG/internal/federation"
)

// TestA2AInboundFederatedPrincipal proves the INBOUND federated path: a call to
// /a2a that carries trusted-proxy federated transport headers and NO bearer
// token is resolved against an ACTIVE + verified relationship and relayed to
// the configured outbound endpoint. An unmatched subject is rejected with 403
// FEDERATION_UNTRUSTED, and a bearer-authenticated caller never falls into the
// federated path.
//
// The relationship is seeded through the same process-shared federation
// lifecycle the Lite data-plane resolver reads (lite.FederationLifecycle), the
// seam documented in lite.go; there is no public admin relationship-create API
// yet, so the test activates through that lifecycle like an operator tool would.
func TestA2AInboundFederatedPrincipal(t *testing.T) {
	var relayHits atomic.Int32
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relayHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":"1","result":{"message":{"parts":[{"kind":"text","text":"a2a-echo"}]}}}`)
	}))
	t.Cleanup(relay.Close)

	lite, err := NewLite(context.Background(), LiteOptions{DSN: "file:a2a-inbound-fed?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	admin := httptest.NewServer(lite.Handler())
	gateway := httptest.NewServer(lite.GatewayHandler())
	t.Cleanup(func() { gateway.Close(); admin.Close(); _ = lite.Close() })

	setupBody := map[string]string{
		"username": "admin", "adminPassword": "password-123456", "tenantName": "Acme",
		"providerName": "Test Provider", "providerType": "openai",
		"providerEndpoint": providerEndpoint(newTestProviderServer(t)), "providerSecret": testProviderKey,
		"selectedModel": "gpt-4o-mini",
	}
	status, body := doJSON(t, admin.URL+"/api/admin/setup", "POST", setupBody, "", "")
	if status != http.StatusOK {
		t.Fatalf("setup status = %d body = %s", status, body)
	}
	var setupResult struct {
		VirtualKey string `json:"virtualKey"`
		TenantID   string `json:"tenantId"`
		ProjectID  string `json:"projectId"`
	}
	if err := json.Unmarshal([]byte(body), &setupResult); err != nil {
		t.Fatal(err)
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

	// Publish a config whose A2A target is a plain local agent reached through
	// the relay. The relay is a plain (non-federated) target so the OUTBOUND
	// federation gate does not apply; only the inbound caller identity is
	// federated.
	publishLocalA2ARelay := func(url string) {
		status, body := doJSON(t, admin.URL+"/api/admin/config/drafts", "POST", nil, cookies, session.CSRFToken)
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
		document["agents"] = []map[string]any{{
			"id": "agent-local", "tenant_id": setupResult.TenantID, "project_id": setupResult.ProjectID,
			"name": "Local Agent", "status": "active", "capabilities": []string{"chat"},
		}}
		document["agent_endpoints"] = []map[string]any{{
			"id": "ep-local", "tenant_id": setupResult.TenantID, "agent_id": "agent-local",
			"version": "1", "url": url, "protocol": "a2a", "capabilities": []string{"chat"},
		}}
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
	}
	publishLocalA2ARelay(relay.URL)

	// Activate an ACTIVE + verified inbound relationship through the shared
	// federation lifecycle the data-plane resolver reads.
	relationship := federation.Relationship{
		ID:               "rel-in-1",
		TenantID:         setupResult.TenantID,
		ExternalAgentID:  "ext-agent-caller",
		Name:             "Partner Caller",
		Anchors:          []federation.TrustAnchor{{ID: "anchor-1", Type: federation.AnchorMTLS, Subject: "spki:partner-cert", Verified: true}},
		ProjectGrants:    []federation.ProjectGrant{{ID: "pg-1", ProjectID: setupResult.ProjectID}},
		CapabilityGrants: []federation.CapabilityGrant{{ID: "cg-1", Capability: "chat"}},
	}
	if _, err := lite.FederationLifecycle().Activate(context.Background(), relationship); err != nil {
		t.Fatal(err)
	}

	strictCall := func(subject string) (int, string) {
		payload := `{"jsonrpc":"2.0","id":"1","method":"message/send","params":{"message":{"messageId":"m1","role":"user","parts":[{"kind":"text","text":"hello"}]}}}`
		return postGatewayFederated(t, gateway.URL+"/a2a", payload, "mtls_spki", subject)
	}
	errorCode := func(body string) string {
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal([]byte(body), &envelope)
		return envelope.Error.Code
	}

	// 1. Matched verified subject -> federated relay succeeds (200 + reply).
	before := relayHits.Load()
	if status, body := strictCall("spki:partner-cert"); status != http.StatusOK {
		t.Fatalf("trusted inbound federated status = %d body = %s, want 200", status, body)
	} else if !strings.Contains(body, "a2a-echo") {
		t.Fatalf("trusted inbound federated body missing relay reply: %s", body)
	}
	if relayHits.Load() <= before {
		t.Fatal("outbound relay was not invoked")
	}

	// 2. Unmatched subject -> rejected fail closed (403 FEDERATION_UNTRUSTED).
	if status, body := strictCall("spki:attacker-cert"); status != http.StatusForbidden || errorCode(body) != "FEDERATION_UNTRUSTED" {
		t.Fatalf("untrusted inbound federated status = %d code = %s body = %s, want 403 FEDERATION_UNTRUSTED", status, errorCode(body), body)
	}

	// 3. A bearer-authenticated caller with the same headers is never treated
	// as federated: it must hit the normal key path, not the resolver.
	payload := map[string]any{
		"messageId": "m2", "role": "user",
		"parts": []map[string]string{{"kind": "text", "text": "hello"}},
	}
	status, body = doGateway(t, gateway.URL+"/a2a", "POST", setupResult.VirtualKey, payload)
	if status != http.StatusOK {
		t.Fatalf("bearer a2a status = %d body = %s, want 200 (key path unaffected)", status, body)
	}
}

// postGatewayFederated sends an A2A request without any bearer token but with
// trusted-proxy federated transport headers.
func postGatewayFederated(t *testing.T, url, body, authMethod, subject string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-A2A-Auth-Method", authMethod)
	req.Header.Set("X-A2A-Subject", subject)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(buf)
}
