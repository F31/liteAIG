package benchmark

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/F31/liteAIG/internal/guardrail/builtin"
)

func TestDefaultCorpusValid(t *testing.T) {
	corpus, err := DefaultCorpus()
	if err != nil {
		t.Fatalf("DefaultCorpus: %v", err)
	}
	if corpus.Version == "" {
		t.Fatal("corpus has no version")
	}
	for _, sample := range corpus.Samples {
		if sample.Input == "" {
			t.Fatalf("sample %q has empty input", sample.Category+"/"+sample.Language)
		}
	}
}

func TestRunReproducible(t *testing.T) {
	engine, err := builtin.New(DefaultPolicy())
	if err != nil {
		t.Fatalf("builtin.New: %v", err)
	}
	corpus, err := DefaultCorpus()
	if err != nil {
		t.Fatalf("DefaultCorpus: %v", err)
	}
	first := Run(engine, corpus)
	second := Run(engine, corpus)
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if !bytes.Equal(a, b) {
		t.Fatal("two runs over the same corpus produced different reports")
	}
	if first.EngineVersion != "1" {
		t.Fatalf("EngineVersion = %q, want 1", first.EngineVersion)
	}
	if first.CorpusVersion != corpus.Version {
		t.Fatalf("CorpusVersion = %q, want %q", first.CorpusVersion, corpus.Version)
	}
	if first.Overall.FalseNegativeRate == 0 && first.Overall.FN != 0 {
		t.Fatal("FNRate is zero while FN is non-zero")
	}
	if first.Overall.FPRate == 0 && first.Overall.FP != 0 {
		t.Fatal("FPRate is zero while FP is non-zero")
	}
}

func TestRunDetectionUsesMatches(t *testing.T) {
	engine, err := builtin.New(DefaultPolicy())
	if err != nil {
		t.Fatalf("builtin.New: %v", err)
	}
	corpus := Corpus{
		Version: "test",
		Samples: []Sample{
			{Input: "email me at a@b.co", Category: "pii", Language: "en", Positive: true},
			{Input: "plain text", Category: "pii", Language: "en", Positive: false},
		},
	}
	report := Run(engine, corpus)
	if report.Overall.TP != 1 || report.Overall.FP != 0 {
		t.Fatalf("TP=%d FP=%d, want TP=1 FP=0", report.Overall.TP, report.Overall.FP)
	}
}

func TestCompareRegression(t *testing.T) {
	prior := Report{Overall: CategoryMetrics{Precision: 0.9, Recall: 0.5}}
	current := Report{Overall: CategoryMetrics{Precision: 0.9, Recall: 0.3}}
	delta := Compare(prior, current)
	if !delta.Regression {
		t.Fatalf("expected regression, got %+v", delta)
	}
	if delta.RecallDelta > -0.05 {
		t.Fatalf("RecallDelta = %f, want < -0.05", delta.RecallDelta)
	}
	noChange := Compare(prior, prior)
	if noChange.Regression {
		t.Fatalf("identical reports flagged as regression: %+v", noChange)
	}
}

func TestWriteReportRoundTrip(t *testing.T) {
	report := Report{
		CorpusVersion: "v1",
		EngineVersion: "1",
		Overall:       CategoryMetrics{TP: 5, FP: 1, TN: 9, FN: 0, Precision: 0.8, Recall: 1.0},
	}
	var buf bytes.Buffer
	if err := WriteReport(&buf, report); err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	loaded, err := LoadReport(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("LoadReport: %v", err)
	}
	if loaded.Overall.TP != report.Overall.TP || loaded.CorpusVersion != report.CorpusVersion {
		t.Fatalf("round trip mismatch: %+v", loaded)
	}
}
