package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestDashboardAndDraftsEndpoints(t *testing.T) {
	_, server, setup, cookies, _ := setupLite(t)
	_ = setup

	// Dashboard aggregates the Setup first call.
	status, body := doJSON(t, server.URL+"/api/admin/dashboard", "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("dashboard status = %d body = %s", status, body)
	}
	var dash struct {
		Requests      int64 `json:"requests"`
		InputTokens   int64 `json:"inputTokens"`
		OutputTokens  int64 `json:"outputTokens"`
		AvgLatencyMS  int64 `json:"avgLatencyMS"`
		ConfigVersion int64 `json:"configVersion"`
	}
	if err := json.Unmarshal([]byte(body), &dash); err != nil {
		t.Fatal(err)
	}
	if dash.Requests < 1 || dash.InputTokens+dash.OutputTokens == 0 || dash.ConfigVersion < 1 {
		t.Fatalf("dashboard incomplete: %+v", dash)
	}

	// Drafts lists the config draft created by Setup (published after the wizard).
	status, body = doJSON(t, server.URL+"/api/admin/config/drafts", "GET", nil, cookies, "")
	if status != http.StatusOK {
		t.Fatalf("drafts status = %d body = %s", status, body)
	}
	if !strings.Contains(body, "published") {
		t.Fatalf("drafts body = %s", body)
	}
}
