package benchmark

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed corpus.json
var defaultCorpusJSON []byte

// DefaultCorpus loads the versioned, label-checked corpus bundled with the
// binary. It is the canonical input for the reproducible benchmark report.
func DefaultCorpus() (Corpus, error) {
	var corpus Corpus
	if err := json.Unmarshal(defaultCorpusJSON, &corpus); err != nil {
		return Corpus{}, fmt.Errorf("embedded corpus: %w", err)
	}
	if err := ValidateCorpus(corpus); err != nil {
		return Corpus{}, err
	}
	return corpus, nil
}

// ValidateCorpus verifies that a corpus is well formed: a version is set, no
// sample is empty, and every sample carries a category and language. It makes
// the reproducibility contract explicit instead of leaking it into the report.
func ValidateCorpus(corpus Corpus) error {
	if corpus.Version == "" {
		return fmt.Errorf("corpus: missing version")
	}
	for i, sample := range corpus.Samples {
		if sample.Input == "" {
			return fmt.Errorf("corpus: sample %d has empty input", i)
		}
		if sample.Category == "" {
			return fmt.Errorf("corpus: sample %d has empty category", i)
		}
		if sample.Language == "" {
			return fmt.Errorf("corpus: sample %d has empty language", i)
		}
	}
	return nil
}
