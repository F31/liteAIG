package loadtest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fakeGateway(t *testing.T, stream bool, mode atomic.Int64) (string, *atomic.Int64, func(string)) {
	t.Helper()
	var seen atomic.Int64
	var sawStream atomic.Bool
	bad := make(chan string, 1)
	validate := func(cfgs []string) {}
	handler := func(w http.ResponseWriter, r *http.Request) {
		seen.Add(1)
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "" {
			sawStream.Store(body.Stream)
		}
		switch mode.Load() {
		case 1:
			select {
			case bad <- r.Header.Get("Authorization"):
			default:
			}
		case 2:
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		if !stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"benchmark response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: " + `{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"bench-"},"finish_reason":null}]}` + "\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: " + `{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"mark"},"finish_reason":"stop"}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)
	return server.URL, &seen, func(s string) {
		validate([]string{s})
	}
}

func TestLoadtestChatMeasuresThroughputAndLatency(t *testing.T) {
	var mode atomic.Int64
	url, _, _ := fakeGateway(t, false, mode)
	report, err := Run(context.Background(), Config{
		BaseURL: url, APIKey: "sk-test", Model: "default-chat",
		Duration: 500 * time.Millisecond, Concurrency: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != "chat" {
		t.Fatalf("mode = %s", report.Mode)
	}
	if report.Requests == 0 || report.RPS == 0 {
		t.Fatalf("report = %+v", report)
	}
	if report.Errors != 0 || report.ErrorRate != 0 {
		t.Fatalf("errors = %d (%.4f)", report.Errors, report.ErrorRate)
	}
	if report.LatencyP99MS <= 0 || report.LatencyP50MS > report.LatencyP99MS {
		t.Fatalf("latencies = %+v", report)
	}
	if !report.SLOMet() {
		t.Fatalf("violations = %v", report.SLOViolations)
	}
}

func TestLoadtestStreamMeasuresTTFT(t *testing.T) {
	var mode atomic.Int64
	url, _, _ := fakeGateway(t, true, mode)
	report, err := Run(context.Background(), Config{
		BaseURL: url, APIKey: "sk-test", Model: "default-chat",
		Duration: 500 * time.Millisecond, Concurrency: 2, Stream: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != "stream" {
		t.Fatalf("mode = %s", report.Mode)
	}
	if report.TTFTP99MS <= 0 {
		t.Fatalf("ttft = %f, want > 0", report.TTFTP99MS)
	}
	if report.LatencyP99MS <= 0 {
		t.Fatalf("latency p99 = %f", report.LatencyP99MS)
	}
}

func TestLoadtestSLOFailure(t *testing.T) {
	var mode atomic.Int64
	url, _, _ := fakeGateway(t, false, mode)
	report, err := Run(context.Background(), Config{
		BaseURL: url, APIKey: "sk-test", Model: "default-chat",
		Duration: 300 * time.Millisecond, Concurrency: 1,
		RPSFloor: 100000, // deliberately unreachable online
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.SLOMet() || len(report.SLOViolations) == 0 {
		t.Fatalf("expected SLO violation, report = %+v", report)
	}
}

func TestLoadtestCarriesBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-carrier" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	report, err := Run(context.Background(), Config{
		BaseURL: server.URL, APIKey: "sk-carrier", Model: "default-chat",
		Duration: 300 * time.Millisecond, Concurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Errors != 0 {
		t.Fatalf("authenticated run failed: %+v", report)
	}
}

func TestLoadtestReportsHTTPErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	t.Cleanup(server.Close)

	report, err := Run(context.Background(), Config{
		BaseURL: server.URL, APIKey: "sk-test", Model: "default-chat",
		Duration: 300 * time.Millisecond, Concurrency: 2, MaxErrorRate: 0.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Errors == 0 || report.ErrorRate != 1.0 {
		t.Fatalf("expected all-429 run to fail, report = %+v", report)
	}
	if !strings.Contains(fmt.Sprint(report.SLOViolations), "errorRate") {
		t.Fatalf("violations = %v", report.SLOViolations)
	}
}
