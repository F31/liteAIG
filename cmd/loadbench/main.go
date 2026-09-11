// Command loadbench drives gateway-level load against a running LiteAIG
// gateway and reports throughput, latency percentiles, and (for SSE streams)
// time-to-first-token. It mirrors the OpenAI-compatible SDK wire contract.
//
// Usage:
//
//	loadbench --gateway http://127.0.0.1:18082 --key sk-lia-... [--stream]
//	         [--duration 10s --concurrency 8 --model default-chat]
//	         [--rps-floor 50 --p99-ms-floor 500 --max-error-rate 0.02]
//	         [--report bench.json] [--json]
//
// SLO flags make the command exit non-zero when the run misses a configured
// floor. Combine --json with --report to save a machine-readable artifact.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/F31/liteAIG/internal/loadtest"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("loadbench", flag.ContinueOnError)
	flags.SetOutput(stderr)
	gateway := flags.String("gateway", "", "gateway base URL (required)")
	key := flags.String("key", "", "virtual API key (required)")
	model := flags.String("model", "default-chat", "model id to send")
	duration := flags.Duration("duration", 10*time.Second, "load window")
	concurrency := flags.Int("concurrency", 8, "parallel workers")
	stream := flags.Bool("stream", false, "use SSE streaming and report TTFT")
	prompts := flags.Int("prompts", 4, "distinct prompt variants to cycle")
	rpsFloor := flags.Float64("rps-floor", 0, "fail if achieved RPS is below this floor")
	p99Floor := flags.Float64("p99-ms-floor", 0, "fail if p99 latency exceeds this floor (ms)")
	maxErrorRate := flags.Float64("max-error-rate", 0.02, "fail if error rate exceeds this ratio")
	reportPath := flags.String("report", "", "path to write a JSON report")
	jsonOut := flags.Bool("json", false, "print the report as JSON on stdout")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *gateway == "" || *key == "" {
		fmt.Fprintln(stderr, "loadbench: --gateway and --key are required")
		return 2
	}

	cfg := loadtest.Config{
		BaseURL: *gateway, APIKey: *key, Model: *model,
		Duration: *duration, Concurrency: *concurrency,
		Stream: *stream, Prompts: *prompts,
		RPSFloor: *rpsFloor, P99MSFloor: *p99Floor, MaxErrorRate: *maxErrorRate,
	}
	report, err := loadtest.Run(context.Background(), cfg)
	if err != nil {
		fmt.Fprintln(stderr, "loadbench:", err)
		return 1
	}

	if *jsonOut || *reportPath != "" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, "loadbench:", err)
			return 1
		}
		if *reportPath != "" {
			if err := os.WriteFile(*reportPath, encoded, 0o644); err != nil {
				fmt.Fprintln(stderr, "loadbench:", err)
				return 1
			}
			fmt.Fprintf(stderr, "report written to %s\n", *reportPath)
		}
		if *jsonOut {
			fmt.Fprintln(stdout, string(encoded))
		}
	} else {
		printHuman(stderr, report)
	}

	for _, violation := range report.SLOViolations {
		fmt.Fprintf(stderr, "SLO violation: %s\n", violation)
	}
	if !report.SLOMet() {
		return 1
	}
	return 0
}

func printHuman(w io.Writer, report loadtest.Report) {
	fmt.Fprintf(w, "mode        : %s\n", report.Mode)
	fmt.Fprintf(w, "duration    : %d ms\n", report.DurationMS)
	fmt.Fprintf(w, "requests    : %d (errors %d, %.2f%%)\n", report.Requests, report.Errors, report.ErrorRate*100)
	fmt.Fprintf(w, "throughput  : %.1f req/s\n", report.RPS)
	fmt.Fprintf(w, "latency     : p50 %.1f ms  p95 %.1f ms  p99 %.1f ms\n",
		report.LatencyP50MS, report.LatencyP95MS, report.LatencyP99MS)
	if report.Mode == "stream" {
		fmt.Fprintf(w, "ttft        : p99 %.1f ms\n", report.TTFTP99MS)
	}
	if report.SLOMet() {
		fmt.Fprintf(w, "SLO         : all met\n")
	} else {
		fmt.Fprintf(w, "SLO         : %d violation(s)\n", len(report.SLOViolations))
		for _, violation := range report.SLOViolations {
			fmt.Fprintf(w, "  - %s\n", violation)
		}
	}
}
