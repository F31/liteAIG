package app

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/F31/liteAIG/internal/access/protocol/a2a"
)

// signCardRS256 signs an Agent Card with the LiteAIG JWS profile (RS256) using
// the same canonicalization the verifier expects: the card marshaled without
// its Signature field, covered by a SHA-256 digest of the JWS signing input.
func signCardRS256(key *rsa.PrivateKey, card *a2a.AgentCard) error {
	unsigned := *card
	unsigned.Signature = nil
	canonical, err := json.Marshal(&unsigned)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(canonical)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	payload := base64.RawURLEncoding.EncodeToString(digest[:])
	signingInput := header + "." + payload
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sha256Digest([]byte(signingInput)))
	if err != nil {
		return err
	}
	card.Signature = &a2a.CardSignature{Alg: "RS256", Value: base64.RawURLEncoding.EncodeToString(signature)}
	return nil
}

func sha256Digest(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func newCardServer(t *testing.T, name, version string, capabilities []string, key *rsa.PrivateKey) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent-card.json" {
			http.NotFound(w, r)
			return
		}
		card := &a2a.AgentCard{
			Name: name, URL: "http://" + r.Host, Version: version,
			Description:  "discovered peer",
			Capabilities: capabilities,
			Publisher:    "test",
		}
		if key != nil {
			if err := signCardRS256(key, card); err != nil {
				t.Error(err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(card)
	}))
	t.Cleanup(server.Close)
	return server
}

func rsaTestKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return key, string(block)
}

// TestA2AAgentCardLifecycleLoop proves the Agent Card → verify → review →
// activate loop closes through the admin surface and the data plane: a signed
// card becomes a candidate, approval with a verified anchor plus grants
// activates the relationship, an inbound federated caller is then admitted,
// and a material card change returns the relationship to pending review.
func TestA2AAgentCardLifecycleLoop(t *testing.T) {
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":"1","result":{"message":{"parts":[{"kind":"text","text":"a2a-echo"}]}}}`)
	}))
	t.Cleanup(relay.Close)

	key, publicPEM := rsaTestKey(t)
	cardV1 := newCardServer(t, "ext-agent", "1", []string{"chat"}, key)
	cardV2 := newCardServer(t, "ext-agent", "2", []string{"chat", "extra"}, key)

	lite, err := NewLite(context.Background(), LiteOptions{DSN: testMemoryDSN(t, "a2a-card")})
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

	// Publish a LOCAL A2A relay endpoint (internal agent) so the inbound
	// federated caller is relayed without an additional outbound gate.
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
	tenantID := document["tenant"].(map[string]any)["id"].(string)
	projectID := document["projects"].([]any)[0].(map[string]any)["id"].(string)
	document["agents"] = []map[string]any{{
		"id": "agent-rel", "tenant_id": tenantID, "project_id": projectID,
		"name": "Relay", "status": "active", "capabilities": []string{"chat"},
	}}
	document["agent_endpoints"] = []map[string]any{{
		"id": "ep-rel", "tenant_id": tenantID, "agent_id": "agent-rel",
		"version": "1", "url": relay.URL, "protocol": "a2a", "capabilities": []string{"chat"},
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

	federatedCall := func() (int, string) {
		payload := `{"messageId":"m1","role":"user","parts":[{"kind":"text","text":"hello"}]}`
		req, err := http.NewRequest(http.MethodPost, gateway.URL+"/a2a", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-A2A-Auth-Method", "jws")
		req.Header.Set("X-A2A-Subject", "ext-agent")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}

	// 1. Before discovery there is no active relationship: fail closed.
	if status, body := federatedCall(); status != http.StatusForbidden || !strings.Contains(body, "FEDERATION_UNTRUSTED") {
		t.Fatalf("pre-discover status = %d body = %s, want 403 FEDERATION_UNTRUSTED", status, body)
	}

	// 2. Discover the signed card → candidate with a verified anchor.
	status, body = doJSON(t, admin.URL+"/api/admin/federation/discover", "POST", map[string]any{
		"url": cardV1.URL, "name": "ext-agent",
		"verificationKeys": []map[string]string{{"alg": "RS256", "pem": publicPEM}},
	}, cookies, session.CSRFToken)
	if status != http.StatusOK {
		t.Fatalf("discover status = %d body = %s", status, body)
	}
	var discovered struct {
		ID                string `json:"id"`
		Status            string `json:"status"`
		HasVerifiedAnchor bool   `json:"hasVerifiedAnchor"`
	}
	if err := json.Unmarshal([]byte(body), &discovered); err != nil {
		t.Fatal(err)
	}
	if discovered.Status != "candidate" || !discovered.HasVerifiedAnchor {
		t.Fatalf("discovered = %+v", discovered)
	}

	// 3. Approval with grants activates the relationship.
	status, body = doJSON(t, admin.URL+"/api/admin/federation/"+discovered.ID+"/review", "POST", map[string]any{
		"approved": true, "projectGrants": []string{projectID}, "capabilityGrants": []string{"chat"},
	}, cookies, session.CSRFToken)
	if status != http.StatusOK {
		t.Fatalf("review status = %d body = %s", status, body)
	}

	// 4. The inbound federated caller is now admitted and relayed.
	if status, body := federatedCall(); status != http.StatusOK || !strings.Contains(body, "a2a-echo") {
		t.Fatalf("post-activate status = %d body = %s, want 200 + echo", status, body)
	}

	// 5. A material card change returns the relationship to pending review and
	// the caller is again denied.
	status, body = doJSON(t, admin.URL+"/api/admin/federation/discover", "POST", map[string]any{
		"url": cardV2.URL, "name": "ext-agent",
		"verificationKeys": []map[string]string{{"alg": "RS256", "pem": publicPEM}},
	}, cookies, session.CSRFToken)
	if status != http.StatusOK {
		t.Fatalf("rediscover status = %d body = %s", status, body)
	}
	if status, body := federatedCall(); status != http.StatusForbidden || !strings.Contains(body, "FEDERATION_UNTRUSTED") {
		t.Fatalf("post-material-change status = %d body = %s, want 403 FEDERATION_UNTRUSTED", status, body)
	}
}
