// Package tokenizer provides a versioned model→encoding registry and token
// estimation for pre-request budget reservation, TPM metering, and Context
// Guard checks. It never replaces provider-reported usage: the pipeline uses
// the highest-priority fact available (provider usage > count API > known
// model tokenizer > conservative estimate).
package tokenizer

import (
	"math"
	"strings"
)

// EstimationMethod values recorded on UnifiedUsage.EstimationMethod.
const (
	MethodProvider     = "provider"
	MethodCountAPI     = "count_api"
	MethodLocal        = "local"
	MethodConservative = "conservative"
)

// RegistryVersion identifies the embedded model→encoding table. Bump it when
// the table changes so accounting can tell estimates from different tables
// apart.
const RegistryVersion = "v1"

// Encoding is one known tokenizer with an observed chars-per-token ratio.
// Ratios are intentionally conservative (slightly fewer chars per token than
// the real tokenizer) so pre-request estimates never undercount.
type Encoding struct {
	Name          string
	CharsPerToken float64
}

// knownEncodings are the encodings used by the models in models.
var knownEncodings = map[string]Encoding{
	"o200k_base":  {Name: "o200k_base", CharsPerToken: 4.0},
	"cl100k_base": {Name: "cl100k_base", CharsPerToken: 4.0},
	"claude":      {Name: "claude", CharsPerToken: 3.8},
	"llama":       {Name: "llama", CharsPerToken: 4.0},
	"gemini":      {Name: "gemini", CharsPerToken: 4.0},
}

// models maps a model name (or family prefix) to its encoding. Lookup uses a
// suffix match so a model family is covered even when the exact revision is
// unknown.
var models = map[string]string{
	"gpt-4o":                 "o200k_base",
	"gpt-4.1":                "o200k_base",
	"o1":                     "o200k_base",
	"o3":                     "o200k_base",
	"gpt-3.5-turbo":          "cl100k_base",
	"gpt-3.5":                "cl100k_base",
	"gpt-4":                  "cl100k_base",
	"text-embedding-3":       "cl100k_base",
	"text-embedding-ada-002": "cl100k_base",
	"claude":                 "claude",
	"llama":                  "llama",
	"mistral":                "llama",
	"qwen":                   "llama",
	"gemini":                 "gemini",
}

// Result carries one estimation plus how it was produced.
type Result struct {
	Tokens  int64
	Method  string
	Version string
}

// Registry estimates tokens for a model.
type Registry struct {
	version   string
	models    map[string]string
	encodings map[string]Encoding
}

// New returns the embedded, versioned Registry.
func New() *Registry {
	modelsCopy := make(map[string]string, len(models))
	for k, v := range models {
		modelsCopy[k] = v
	}
	encCopy := make(map[string]Encoding, len(knownEncodings))
	for k, v := range knownEncodings {
		encCopy[k] = v
	}
	return &Registry{version: RegistryVersion, models: modelsCopy, encodings: encCopy}
}

// Version returns the registry version string.
func (r *Registry) Version() string {
	if r == nil {
		return RegistryVersion
	}
	return r.version
}

// EstimateRunes estimates tokens for model given a rune count without
// materializing the text. Unknown models get the conservative fallback.
func (r *Registry) EstimateRunes(model string, runes int) Result {
	if r == nil {
		return conservativeEstimateRunes(runes)
	}
	encoding, ok := r.encodingFor(model)
	if !ok {
		return conservativeEstimateRunes(runes)
	}
	tokens := int64(math.Ceil(float64(runes) / encoding.CharsPerToken))
	if tokens < 1 {
		tokens = 1
	}
	return Result{Tokens: tokens, Method: MethodLocal, Version: r.version}
}

// EstimateTokens estimates how many tokens text occupies for model. It returns
// a conservative estimate when the model or its encoding is unknown.
func (r *Registry) EstimateTokens(model, text string) Result {
	return r.EstimateRunes(model, len([]rune(text)))
}

// encodingFor resolves the encoding for a model by longest prefix match.
func (r *Registry) encodingFor(model string) (Encoding, bool) {
	model = strings.ToLower(model)
	best := ""
	for key := range r.models {
		k := strings.ToLower(key)
		if strings.HasPrefix(model, k) && len(k) > len(best) {
			best = key
		}
	}
	if best == "" {
		return Encoding{}, false
	}
	enc, ok := r.encodings[r.models[best]]
	return enc, ok
}

// conservativeEstimate is the fallback from spec §12.2:
//
//	estimate = ceil(utf8_runes / 3.3) * safety_factor
//	safety_factor default = 1.2
func conservativeEstimate(text string) Result {
	return conservativeEstimateRunes(len([]rune(text)))
}

func conservativeEstimateRunes(runes int) Result {
	const perToken = 3.3
	const safetyFactor = 1.2
	tokens := int64(math.Ceil(math.Ceil(float64(runes)/perToken) * safetyFactor))
	if tokens < 1 {
		tokens = 1
	}
	return Result{Tokens: tokens, Method: MethodConservative, Version: RegistryVersion}
}
