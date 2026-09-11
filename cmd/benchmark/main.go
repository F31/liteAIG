// Command benchmark runs the versioned guardrail corpus through the builtin
// engine and writes a reproducible JSON report with regression deltas versus a
// prior baseline.
//
// Usage:
//
//	benchmark [--corpus corpus.json] [--baseline report.json] [--out report.json] [--fail-on-regression]
//
// Without --corpus the bundled, label-checked corpus is used. With --baseline
// the overall precision/recall deltas are printed; a material regression
// (>= 5 points) exits non-zero unless --fail-on-regression=false.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/F31/liteAIG/internal/guardrail/benchmark"
	"github.com/F31/liteAIG/internal/guardrail/builtin"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("benchmark", flag.ContinueOnError)
	flags.SetOutput(stderr)
	corpusPath := flags.String("corpus", "", "path to a corpus JSON file (default: bundled versioned corpus)")
	baselinePath := flags.String("baseline", "", "path to a prior report JSON to compare regression deltas against")
	outPath := flags.String("out", "", "path to write the JSON report to (default: stdout)")
	failOnRegression := flags.Bool("fail-on-regression", true, "exit non-zero when the benchmark regresses vs the baseline")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	corpus, corpusLabel, err := loadCorpus(*corpusPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	engine, err := builtin.New(benchmark.DefaultPolicy())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	report := benchmark.Run(engine, corpus)
	if *corpusPath == "" {
		fmt.Fprintf(stderr, "running bundled corpus %s\n", corpusLabel)
	}
	var encoded bytes.Buffer
	if err := benchmark.WriteReport(&encoded, report); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *outPath != "" {
		if err := os.WriteFile(*outPath, encoded.Bytes(), 0o644); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stderr, "benchmark report written to %s\n", *outPath)
	} else {
		if _, err := stdout.Write(encoded.Bytes()); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}

	if *baselinePath == "" {
		return 0
	}
	return compare(report, *baselinePath, *failOnRegression, stderr)
}

func loadCorpus(path string) (benchmark.Corpus, string, error) {
	if path == "" {
		corpus, err := benchmark.DefaultCorpus()
		if err != nil {
			return benchmark.Corpus{}, "", err
		}
		return corpus, corpus.Version, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return benchmark.Corpus{}, "", fmt.Errorf("read corpus: %w", err)
	}
	var corpus benchmark.Corpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		return benchmark.Corpus{}, "", fmt.Errorf("parse corpus %s: %w", path, err)
	}
	if err := benchmark.ValidateCorpus(corpus); err != nil {
		return benchmark.Corpus{}, "", err
	}
	return corpus, path, nil
}

func compare(report benchmark.Report, baselinePath string, failOnRegression bool, stderr io.Writer) int {
	baseline, err := loadReportFile(baselinePath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	delta := benchmark.Compare(baseline, report)
	fmt.Fprintf(stderr, "delta vs %s: precision %+.2f recall %+.2f\n", baselinePath, delta.PrecisionDelta, delta.RecallDelta)
	if delta.Regression {
		fmt.Fprintln(stderr, delta.Message)
	}
	if delta.Regression && failOnRegression {
		return 1
	}
	return 0
}

func loadReportFile(path string) (benchmark.Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return benchmark.Report{}, fmt.Errorf("read baseline: %w", err)
	}
	return benchmark.LoadReport(bytes.NewReader(data))
}
