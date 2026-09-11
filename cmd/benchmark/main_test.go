package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/F31/liteAIG/internal/guardrail/benchmark"
)

func TestRunBundledWritesReport(t *testing.T) {
	out := filepath.Join(t.TempDir(), "report.json")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--out", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr: %s", code, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report benchmark.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("parse report: %v", err)
	}
	if report.EngineVersion == "" || report.CorpusVersion == "" {
		t.Fatalf("report missing version fields: %+v", report)
	}
}

func TestRunWritesToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr: %s", code, stderr.String())
	}
	if len(stdout.Bytes()) == 0 {
		t.Fatal("expected report on stdout")
	}
	var report benchmark.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("parse stdout report: %v", err)
	}
	if report.Overall.TP == 0 {
		t.Fatalf("expected positive detections, got %+v", report.Overall)
	}
}

func TestRunRegressionExitsNonZero(t *testing.T) {
	dir := t.TempDir()
	corpusPath := filepath.Join(dir, "corpus.json")
	if err := os.WriteFile(corpusPath, []byte(`{"version":"t","params":"t","samples":[
	{"input":"contact a@b.co","category":"pii","language":"en","positive":false}
]}`), 0o644); err != nil {
		t.Fatalf("write corpus: %v", err)
	}
	baseline := filepath.Join(dir, "baseline.json")
	blob, _ := json.Marshal(benchmark.Report{Overall: reportMetrics(1.0, 1.0)})
	if err := os.WriteFile(baseline, blob, 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"--corpus", corpusPath, "--baseline", baseline}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit 1 on regression, got %d (stderr: %s)", code, stderr.String())
	}
}

func TestRunRegressionPassesWhenAllowed(t *testing.T) {
	dir := t.TempDir()
	corpusPath := filepath.Join(dir, "corpus.json")
	if err := os.WriteFile(corpusPath, []byte(`{"version":"t","params":"t","samples":[
	{"input":"contact a@b.co","category":"pii","language":"en","positive":false}
]}`), 0o644); err != nil {
		t.Fatalf("write corpus: %v", err)
	}
	baseline := filepath.Join(dir, "baseline.json")
	blob, _ := json.Marshal(benchmark.Report{Overall: reportMetrics(1.0, 1.0)})
	if err := os.WriteFile(baseline, blob, 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"--corpus", corpusPath, "--baseline", baseline, "--fail-on-regression=false"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0 with fail-on-regression=false, got %d (stderr: %s)", code, stderr.String())
	}
}

func reportMetrics(precision, recall float64) benchmark.CategoryMetrics {
	return benchmark.CategoryMetrics{Precision: precision, Recall: recall}
}
