// Package loadtest drives gateway-level load against a running LiteAIG data
// plane (/v1/chat/completions, non-streaming and SSE streaming) and reports
// throughput, latency percentiles, streaming TTFT, and SLO assertions.
//
// The harness is intentionally small and dependency-free: it speaks the same
// wire contract as the OpenAI-compatible SDK and can target any standalone
// gateway URL, or an in-process httptest.Server in unit tests.
package loadtest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config describes one benchmark run.
type Config struct {
	BaseURL      string        // gateway base (e.g. http://127.0.0.1:18082)
	APIKey       string        // virtual key; sent as Authorization: Bearer <key>
	Model        string        // model id sent in the request (default `default-chat`)
	Duration     time.Duration // how long to drive load (default 10s)
	Concurrency  int           // parallel workers (default 8)
	Stream       bool          // use SSE streaming and measure TTFT
	Prompts      int           // distinct prompts to cyclically vary (default 4)
	RPSFloor     float64       // SLO: exit non-zero if achieved RPS < floor
	P99MSFloor   float64       // SLO: exit non-zero if p99 latency > floor
	MaxErrorRate float64       // SLO: exit non-zero if error rate > floor (default 0.02)

	client *http.Client // test override
}

func (c *Config) fill() {
	if c.BaseURL == "" {
		panic("loadtest: BaseURL is required")
	}
	if c.Duration <= 0 {
		c.Duration = 10 * time.Second
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 8
	}
	if c.Model == "" {
		c.Model = "default-chat"
	}
	if c.Prompts <= 0 {
		c.Prompts = 4
	}
	if c.MaxErrorRate == 0 {
		c.MaxErrorRate = 0.02
	}
}

// Report is the aggregate result of one run plus SLO verdicts.
type Report struct {
	Mode          string         `json:"mode"` // chat | stream
	DurationMS    int64          `json:"durationMs"`
	Requests      int            `json:"requests"`
	Errors        int            `json:"errors"`
	ErrorRate     float64        `json:"errorRate"`
	RPS           float64        `json:"rps"`
	LatencyP50MS  float64        `json:"latencyP50Ms"`
	LatencyP95MS  float64        `json:"latencyP95Ms"`
	LatencyP99MS  float64        `json:"latencyP99Ms"`
	TTFTP99MS     float64        `json:"ttftP99Ms"` // stream only; 0 for chat
	ErrorSummary  map[string]int `json:"errorSummary,omitempty"`
	SLOViolations []string       `json:"sloViolations,omitempty"`
}

// SLOMet reports whether every configured SLO held.
func (r Report) SLOMet() bool { return len(r.SLOViolations) == 0 }

// Run executes the load window and returns the aggregate report.
func Run(ctx context.Context, cfg Config) (Report, error) {
	cfg.fill()
	client := cfg.client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	deadline := time.Now().Add(cfg.Duration)
	benchStart := time.Now()

	var (
		mu        sync.Mutex
		latencies []time.Duration
		ttfts     []time.Duration
		errorsBy  map[string]int
		requests  int
		failures  int
	)
	errorsBy = map[string]int{}
	var wg sync.WaitGroup
	var counter atomic.Int64
	sem := make(chan struct{}, cfg.Concurrency)
loop:
	for {
		select {
		case <-ctx.Done():
			return Report{}, ctx.Err()
		case <-time.After(time.Until(deadline)):
			break loop
		default:
		}
		select {
		case sem <- struct{}{}:
		default:
			time.Sleep(2 * time.Millisecond)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			promptNum := int(counter.Add(1))
			latency, ttft, err := runOnce(ctx, client, cfg, promptNum)
			mu.Lock()
			requests++
			if err != nil {
				failures++
				errorsBy[err.Error()]++
			} else {
				latencies = append(latencies, latency)
				if ttft > 0 {
					ttfts = append(ttfts, ttft)
				}
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	elapsed := time.Since(benchStart)

	mode := "chat"
	if cfg.Stream {
		mode = "stream"
	}
	report := Report{Mode: mode, DurationMS: elapsed.Milliseconds(), Requests: requests, Errors: failures}
	if len(errorsBy) > 0 {
		report.ErrorSummary = errorsBy
	}
	if requests > 0 {
		report.RPS = float64(requests) / elapsed.Seconds()
		report.ErrorRate = float64(failures) / float64(requests)
	}
	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		report.LatencyP50MS = percentileMS(latencies, 0.50)
		report.LatencyP95MS = percentileMS(latencies, 0.95)
		report.LatencyP99MS = percentileMS(latencies, 0.99)
	}
	if len(ttfts) > 0 {
		sort.Slice(ttfts, func(i, j int) bool { return ttfts[i] < ttfts[j] })
		report.TTFTP99MS = percentileMS(ttfts, 0.99)
	}
	report.SLOViolations = sloViolations(cfg, report)
	return report, nil
}

func runOnce(ctx context.Context, client *http.Client, cfg Config, promptNum int) (time.Duration, time.Duration, error) {
	prompt := fmt.Sprintf("benchmark prompt %d: measure the gateway end to end.", promptNum%cfg.Prompts)
	payload := map[string]any{
		"model":    cfg.Model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	}
	if cfg.Stream {
		payload["stream"] = true
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, -1, err
	}
	start := time.Now()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return 0, -1, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	response, err := client.Do(request)
	if err != nil {
		return time.Since(start), -1, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return time.Since(start), -1, errors.New("gateway returned status " + response.Status)
	}
	if !cfg.Stream {
		if _, err := io.Copy(io.Discard, response.Body); err != nil {
			return time.Since(start), -1, err
		}
		return time.Since(start), -1, nil
	}
	// Streaming: first SSE `data:` frame marks TTFT; the full read marks latency.
	var ttft time.Duration = -1
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if ttft < 0 && strings.HasPrefix(line, "data: ") {
			ttft = time.Since(start)
		}
		if line == "data: [DONE]" {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return time.Since(start), ttft, err
	}
	return time.Since(start), ttft, nil
}

func sloViolations(cfg Config, report Report) []string {
	var violations []string
	if cfg.RPSFloor > 0 && report.RPS < cfg.RPSFloor {
		violations = append(violations, fmt.Sprintf("rps %.1f < floor %.1f", report.RPS, cfg.RPSFloor))
	}
	if cfg.P99MSFloor > 0 && report.LatencyP99MS > cfg.P99MSFloor {
		violations = append(violations, fmt.Sprintf("p99 %.1fms > floor %.1fms", report.LatencyP99MS, cfg.P99MSFloor))
	}
	if cfg.MaxErrorRate > 0 && report.ErrorRate > cfg.MaxErrorRate {
		violations = append(violations, fmt.Sprintf("errorRate %.4f > %.4f", report.ErrorRate, cfg.MaxErrorRate))
	}
	return violations
}

func percentileMS(samples []time.Duration, q float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	idx := int(q * float64(len(samples)-1))
	if idx < 0 {
		idx = 0
	}
	return float64(samples[idx].Microseconds()) / 1000.0
}

// ConfigOpt mutates a Config before it is filled.
type ConfigOpt func(*Config)

// NewConfig builds a benchmark config with overrides applied.
func NewConfig(baseURL, apiKey string, duration time.Duration, stream bool, opts ...ConfigOpt) Config {
	cfg := Config{BaseURL: baseURL, APIKey: apiKey, Duration: duration, Stream: stream}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithClient overrides the HTTP client (used by tests for the fake gateway).
func WithClient(client *http.Client) ConfigOpt { return func(c *Config) { c.client = client } }
