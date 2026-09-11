// Package benchmark runs a versioned corpus through the builtin guardrail
// engine and reports precision/recall/FP/FN by category and language, with
// regression deltas versus a prior version. Reports never promise zero
// false positives or false negatives.
package benchmark

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/F31/liteAIG/internal/guardrail/builtin"
)

// Sample is one labeled corpus item.
type Sample struct {
	Input    string `json:"input"`
	Category string `json:"category"`
	Language string `json:"language"`
	Positive bool   `json:"positive"` // true = should be flagged
}

// Corpus is a versioned labeled benchmark set.
type Corpus struct {
	Version string   `json:"version"`
	Samples []Sample `json:"samples"`
	Params  string   `json:"params"` // thresholds/test parameters
}

// CategoryMetrics is one category/language slice of results.
type CategoryMetrics struct {
	Category          string  `json:"category"`
	Language          string  `json:"language"`
	TP                int     `json:"tp"`
	FP                int     `json:"fp"`
	TN                int     `json:"tn"`
	FN                int     `json:"fn"`
	Precision         float64 `json:"precision"`
	Recall            float64 `json:"recall"`
	FPRate            float64 `json:"false_positive_rate"`
	FalseNegativeRate float64 `json:"false_negative_rate"`
}

// Report is a versioned benchmark result.
type Report struct {
	CorpusVersion string            `json:"corpus_version"`
	EngineVersion string            `json:"engine_version"`
	Params        string            `json:"params"`
	Category      []CategoryMetrics `json:"category"`
	Overall       CategoryMetrics   `json:"overall"`
}

// Run executes the corpus through the engine and computes metrics.
func Run(engine *builtin.Engine, corpus Corpus) Report {
	overall := CategoryMetrics{Category: "all", Language: "all"}
	byCategory := map[string]*CategoryMetrics{}
	var order []string
	for _, sample := range corpus.Samples {
		key := sample.Category + "\x00" + sample.Language
		metrics, ok := byCategory[key]
		if !ok {
			metrics = &CategoryMetrics{Category: sample.Category, Language: sample.Language}
			byCategory[key] = metrics
			order = append(order, key)
		}
		flagged := len(engine.Evaluate(sample.Input).Matches) > 0
		record(metrics, sample.Positive, flagged)
		record(&overall, sample.Positive, flagged)
	}
	report := Report{
		CorpusVersion: corpus.Version,
		EngineVersion: fmt.Sprintf("%d", engine.Version()),
		Params:        corpus.Params,
		Overall:       overall,
	}
	for _, key := range order {
		metrics := byCategory[key]
		finalize(metrics)
		report.Category = append(report.Category, *metrics)
	}
	finalize(&report.Overall)
	return report
}

func record(metrics *CategoryMetrics, positive, flagged bool) {
	switch {
	case positive && flagged:
		metrics.TP++
	case positive && !flagged:
		metrics.FN++
	case !positive && flagged:
		metrics.FP++
	default:
		metrics.TN++
	}
}

func finalize(metrics *CategoryMetrics) {
	metrics.Precision = rate(metrics.TP, metrics.TP+metrics.FP)
	metrics.Recall = rate(metrics.TP, metrics.TP+metrics.FN)
	metrics.FPRate = rate(metrics.FP, metrics.FP+metrics.TN)
	metrics.FalseNegativeRate = rate(metrics.FN, metrics.TP+metrics.FN)
}

func rate(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

// Delta compares two reports and flags material regressions.
type Delta struct {
	PrecisionDelta float64
	RecallDelta    float64
	Regression     bool
	Message        string
}

// Compare computes the regression delta between a prior and current report.
func Compare(prior, current Report) Delta {
	delta := Delta{
		PrecisionDelta: current.Overall.Precision - prior.Overall.Precision,
		RecallDelta:    current.Overall.Recall - prior.Overall.Recall,
	}
	if delta.RecallDelta < -0.05 || delta.PrecisionDelta < -0.05 {
		delta.Regression = true
		delta.Message = fmt.Sprintf("regression: precision %+.2f recall %+.2f", delta.PrecisionDelta, delta.RecallDelta)
	}
	return delta
}

// WriteReport serializes a report as indented JSON suitable for a CI/release
// artifact.
func WriteReport(w io.Writer, report Report) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

// LoadReport reads a prior report produced by WriteReport and restores it for
// regression comparison.
func LoadReport(r io.Reader) (Report, error) {
	var report Report
	err := json.NewDecoder(r).Decode(&report)
	return report, err
}
