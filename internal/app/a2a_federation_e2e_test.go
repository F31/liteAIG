package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestA2AFederationTrustEnforced proves outbound A2A calls to an external
// federated agent are gated by federated trust: an active verified relationship
// admits the call, a suspended/unverified relationship is rejected
// (fail closed), and the delegation hop limit bounds the chain.
func TestA2AFederationTrustEnforced(t *testing.T) {
	a2a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":"1","result":{"message":{"parts":[{"kind":"text","text":"a2a-echo"}]}}}`)
	}))
	t.Cleanup(a2a.Close)

	lite, err := NewLite(context.Background(), LiteOptions{DSN: "file:a2a-federation?mode=memory&cache=shared"})
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

	// Publish a config where the A2A target agent is an external federated
	// agent with a trust relationship that declares full governance facts.
	// mutate (when non-nil) tweaks the document after the defaults are applied,
	// so each sub-case publishes a fresh draft with the same baseline.
	publish := func(mutate func(document map[string]any, tenantID, projectID string)) {
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
		tenantID := document["tenant"].(map[string]any)["id"].(string)
		projectID := document["projects"].([]any)[0].(map[string]any)["id"].(string)
		document["agents"] = []map[string]any{{
			"id": "agent-ext", "tenant_id": tenantID, "project_id": projectID,
			"name": "Ext Agent", "status": "active", "capabilities": []string{"chat"},
		}}
		document["agent_endpoints"] = []map[string]any{{
			"id": "ep-ext", "tenant_id": tenantID, "agent_id": "agent-ext",
			"version": "1", "url": a2a.URL, "protocol": "a2a", "capabilities": []string{"chat"},
		}}
		document["federated_agents"] = []map[string]any{{
			"id": "agent-ext", "tenant_id": tenantID, "name": "Ext",
			"external_subject": "ext-subj", "trust_boundary": "external_federated", "status": "active",
		}}
		document["federation_relationships"] = []map[string]any{{
			"id": "rel-1", "tenant_id": tenantID, "external_agent_id": "agent-ext",
			"status": "active", "assurance_level": "high", "has_verified_anchor": true,
			"direction": "outbound", "project_grants": []string{projectID}, "capability_grants": []string{"chat"},
		}}
		if mutate != nil {
			mutate(document, tenantID, projectID)
		}
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

	a2aCall := func(metadata map[string]string) (int, string) {
		payload := map[string]any{
			"messageId": "m1", "role": "user",
			"parts": []map[string]string{{"kind": "text", "text": "hello"}},
		}
		if metadata != nil {
			payload["metadata"] = metadata
		}
		return doGateway(t, gateway.URL+"/a2a", http.MethodPost, setupResult.VirtualKey, payload)
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

	errorMessage := func(body string) string {
		var envelope struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal([]byte(body), &envelope)
		return envelope.Error.Message
	}

	assertDenied := func(status int, body, wantReason string) {
		if status != http.StatusForbidden || errorCode(body) != "FEDERATION_DENIED" {
			t.Fatalf("denial status = %d code = %s body = %s, want 403 FEDERATION_DENIED", status, errorCode(body), body)
		}
		if !strings.Contains(errorMessage(body), wantReason) {
			t.Fatalf("denial body = %s, want message containing %q", body, wantReason)
		}
	}

	// 1. Active + verified relationship with project/capability grants
	//    declared → the call is admitted.
	publish(nil)
	if status, body := a2aCall(nil); status != http.StatusOK {
		t.Fatalf("trusted a2a status = %d body = %s, want 200", status, body)
	}
	// 2. Suspended relationship → rejected fail closed.
	publish(func(document map[string]any, _, _ string) {
		document["federation_relationships"].([]map[string]any)[0]["status"] = "suspended"
	})
	if status, body := a2aCall(nil); status != http.StatusForbidden || errorCode(body) != "FEDERATION_UNTRUSTED" {
		t.Fatalf("suspended a2a status = %d code = %s body = %s, want 403 FEDERATION_UNTRUSTED", status, errorCode(body), body)
	}
	// 3. Active but unverified anchor → rejected fail closed.
	publish(func(document map[string]any, _, _ string) {
		document["federation_relationships"].([]map[string]any)[0]["has_verified_anchor"] = false
	})
	if status, body := a2aCall(nil); status != http.StatusForbidden || errorCode(body) != "FEDERATION_UNTRUSTED" {
		t.Fatalf("unverified a2a status = %d code = %s body = %s, want 403 FEDERATION_UNTRUSTED", status, errorCode(body), body)
	}
	// 4. Active + verified again, but the delegation depth exceeds the hop limit.
	publish(nil)
	if status, body := a2aCall(map[string]string{"a2a.delegation.hops": "10"}); status != http.StatusForbidden || errorCode(body) != "FEDERATION_HOP_LIMIT" {
		t.Fatalf("hop-limit a2a status = %d code = %s body = %s, want 403 FEDERATION_HOP_LIMIT", status, errorCode(body), body)
	}
	// 5. Within the hop limit → admitted.
	if status, body := a2aCall(map[string]string{"a2a.delegation.hops": "1"}); status != http.StatusOK {
		t.Fatalf("within-hop a2a status = %d body = %s, want 200", status, body)
	}
	// 6. Active + verified but the relationship grants a different project →
	//    rejected because the calling project has no project grant.
	publish(func(document map[string]any, _, _ string) {
		document["federation_relationships"].([]map[string]any)[0]["project_grants"] = []string{"some-other-project"}
	})
	if status, body := a2aCall(nil); errorCode(body) != "FEDERATION_DENIED" {
		t.Fatalf("missing project grant status = %d code = %s body = %s, want FEDERATION_DENIED", status, errorCode(body), body)
	} else {
		assertDenied(status, body, "project grant")
	}
	// 7. Active + verified + project granted, but capability "chat" is not
	//    granted → rejected.
	publish(func(document map[string]any, _, _ string) {
		document["federation_relationships"].([]map[string]any)[0]["capability_grants"] = []string{"read"}
	})
	if status, body := a2aCall(nil); errorCode(body) != "FEDERATION_DENIED" {
		t.Fatalf("missing capability grant status = %d code = %s body = %s, want FEDERATION_DENIED", status, errorCode(body), body)
	} else {
		assertDenied(status, body, "capability grant")
	}
	// 8. Boundary conflict: the relationship processes data only in eu-central-1
	//    while the calling project only allows us-east-1 → rejected.
	publish(func(document map[string]any, _, _ string) {
		relationship := document["federation_relationships"].([]map[string]any)[0]
		relationship["processing_regions"] = []string{"eu-central-1"}
		project := document["projects"].([]any)[0].(map[string]any)
		project["allowed_data_regions"] = []string{"us-east-1"}
		project["residency_enforcement"] = "advisory"
	})
	if status, body := a2aCall(nil); errorCode(body) != "FEDERATION_DENIED" {
		t.Fatalf("boundary conflict status = %d code = %s body = %s, want FEDERATION_DENIED", status, errorCode(body), body)
	} else {
		assertDenied(status, body, "data boundary")
	}
	// 9. Approved-version pinning: the relationship approves version "2" but
	//    the endpoint serves version "1" → rejected.
	publish(func(document map[string]any, _, _ string) {
		document["federation_relationships"].([]map[string]any)[0]["approved_version"] = "2"
	})
	if status, body := a2aCall(nil); errorCode(body) != "FEDERATION_DENIED" {
		t.Fatalf("version pin status = %d code = %s body = %s, want FEDERATION_DENIED", status, errorCode(body), body)
	} else {
		assertDenied(status, body, "approved version")
	}
	// 10. A plain active + verified relationship (fresh publish) admits again.
	publish(nil)
	if status, body := a2aCall(nil); status != http.StatusOK {
		t.Fatalf("restored trusted a2a status = %d body = %s, want 200", status, body)
	}
}
