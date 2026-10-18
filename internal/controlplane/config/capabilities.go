package config

import "strings"

// Canonical model capability vocabulary. This is the single source of truth
// shared by config validation/canonicalization, the resource catalog seeds,
// the routing hard constraints, and the Console capability editor. Values are
// lower-case tokens; "text" models are simply the chat capability.
const (
	CapabilityChat      = "chat"
	CapabilityVision    = "vision"
	CapabilityReasoning = "reasoning"
	CapabilityTools     = "tools"
	CapabilityEmbedding = "embedding"
	CapabilityRerank    = "rerank"
	CapabilityAudio     = "audio"
	CapabilitySpeech    = "speech"
)

// CapabilityVocabulary lists the canonical capabilities in display order.
func CapabilityVocabulary() []string {
	return []string{
		CapabilityChat, CapabilityVision, CapabilityReasoning, CapabilityTools,
		CapabilityEmbedding, CapabilityRerank, CapabilityAudio, CapabilitySpeech,
	}
}

// capabilityAliases maps legacy or provider-specific tokens onto the canonical
// vocabulary so drafts written before the vocabulary existed keep working.
var capabilityAliases = map[string]string{
	"embeddings":       CapabilityEmbedding,
	"embed":            CapabilityEmbedding,
	"multimodal":       CapabilityVision,
	"vision-language":  CapabilityVision,
	"vlm":              CapabilityVision,
	"image":            CapabilityVision,
	"function_calling": CapabilityTools,
	"function-calling": CapabilityTools,
	"tool_calling":     CapabilityTools,
	"tool-calling":     CapabilityTools,
	"stt":              CapabilityAudio,
	"transcription":    CapabilityAudio,
	"translation":      CapabilityAudio,
	"asr":              CapabilityAudio,
	"tts":              CapabilitySpeech,
	"text-to-speech":   CapabilitySpeech,
	"speech-synthesis": CapabilitySpeech,
}

// NormalizeCapability lower-cases, trims and alias-maps one capability token.
func NormalizeCapability(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if alias, ok := capabilityAliases[value]; ok {
		value = alias
	}
	return value
}

// CanonicalCapabilities normalizes a capability list: alias-maps, trims,
// de-duplicates and drops empties while preserving order.
func CanonicalCapabilities(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		value := NormalizeCapability(raw)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// KnownCapability reports whether value is a canonical, recognized capability.
func KnownCapability(value string) bool {
	switch value {
	case CapabilityChat, CapabilityVision, CapabilityReasoning, CapabilityTools,
		CapabilityEmbedding, CapabilityRerank, CapabilityAudio, CapabilitySpeech:
		return true
	default:
		return false
	}
}

// HasCapability reports whether a capability list contains want after
// normalization.
func HasCapability(values []string, want string) bool {
	want = NormalizeCapability(want)
	for _, value := range values {
		if NormalizeCapability(value) == want {
			return true
		}
	}
	return false
}

// InferCapabilities is a best-effort mapping from an upstream model name to a
// canonical capability list. It is used when the setup wizard has no catalog
// capability metadata (the authoritative source is resource_model_catalog);
// operators can always override the result in the Console.
func InferCapabilities(model string) []string {
	lower := strings.ToLower(strings.TrimSpace(model))
	switch {
	case lower == "":
		return []string{CapabilityChat}
	case strings.Contains(lower, "whisper"), strings.Contains(lower, "-asr"), strings.Contains(lower, "transcrib"):
		return []string{CapabilityAudio}
	case strings.Contains(lower, "tts"), strings.Contains(lower, "speech"):
		return []string{CapabilitySpeech}
	case strings.Contains(lower, "rerank"):
		return []string{CapabilityRerank}
	case strings.Contains(lower, "embed"), strings.Contains(lower, "bge-"), strings.Contains(lower, "text-embedding"):
		return []string{CapabilityEmbedding}
	}
	caps := []string{CapabilityChat}
	if strings.Contains(lower, "vision") || strings.Contains(lower, "gpt-4o") ||
		strings.Contains(lower, "gpt-4.1") || strings.Contains(lower, "gpt-5") ||
		strings.Contains(lower, "claude-3") || strings.Contains(lower, "claude-4") ||
		strings.Contains(lower, "gemini") || strings.Contains(lower, "omni") {
		caps = append(caps, CapabilityVision)
	}
	if strings.Contains(lower, "o1") || strings.Contains(lower, "o3") ||
		strings.Contains(lower, "o4") || strings.Contains(lower, "reason") ||
		strings.Contains(lower, "deepseek-r") {
		caps = append(caps, CapabilityReasoning)
	}
	return caps
}
