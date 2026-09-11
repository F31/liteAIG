package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/platform/webkit"
)

func TestDrainGateWaitsForInflightAndRefusesNew(t *testing.T) {
	lifecycle, err := NewLifecycle(DrainConfig{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	finished := make(chan struct{})
	gated := webkit.New()
	gated.Use(drainGate(lifecycle))
	gated.Handle("GET /", func(c *webkit.Context) error {
		defer close(finished)
		<-release
		return c.Text(http.StatusOK, "ok")
	})

	started := make(chan struct{})
	go func() {
		close(started)
		gated.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	<-started
	time.Sleep(50 * time.Millisecond) // let the request register in-flight

	drainDone := make(chan error, 1)
	go func() { drainDone <- lifecycle.BeginDrain(context.Background()) }()

	select {
	case <-drainDone:
		t.Fatal("drain finished before the in-flight request completed")
	case <-time.After(150 * time.Millisecond):
	}
	if lifecycle.Running() {
		t.Fatal("lifecycle still running while draining")
	}

	// New requests are refused while draining.
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status while draining = %d, want 503", rec.Code)
	}

	close(release)
	<-finished
	select {
	case <-drainDone:
	case <-time.After(5 * time.Second):
		t.Fatal("drain did not complete after in-flight finished")
	}
}

func TestLiteDrainWaitsAndRefusesTraffic(t *testing.T) {
	lite, server, _, cookies, csrf := setupLite(t)

	status, body := doJSON(t, server.URL+"/api/admin/playground", "POST", map[string]any{
		"model": "default-chat", "input": "hello", "stream": false,
	}, cookies, csrf)
	if status != http.StatusOK {
		t.Fatalf("playground before drain = %d body = %s", status, body)
	}

	drainDone := make(chan error, 1)
	go func() { drainDone <- lite.Lifecycle().BeginDrain(context.Background()) }()
	deadline := time.Now().Add(2 * time.Second)
	for lite.Lifecycle().Running() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if lite.Lifecycle().Running() {
		t.Fatal("lifecycle did not transition to draining")
	}

	status, body = doJSON(t, server.URL+"/api/admin/playground", "POST", map[string]any{
		"model": "default-chat", "input": "hello", "stream": false,
	}, cookies, csrf)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("playground while draining = %d body = %s, want 503", status, body)
	}

	select {
	case <-drainDone:
	case <-time.After(5 * time.Second):
		t.Fatal("drain did not complete")
	}
	if lite.Lifecycle().Ready() {
		t.Fatal("lifecycle ready after drain")
	}
}
