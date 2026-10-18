package config

import (
	"reflect"
	"testing"
)

func TestCanonicalCapabilities(t *testing.T) {
	got := CanonicalCapabilities([]string{"Chat", "embeddings", "VISION", "chat", "", " tool_calling "})
	want := []string{CapabilityChat, CapabilityEmbedding, CapabilityVision, CapabilityTools}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CanonicalCapabilities() = %v, want %v", got, want)
	}
	if CanonicalCapabilities(nil) != nil {
		t.Fatal("nil capabilities must stay nil")
	}
}

func TestHasCapabilityNormalizes(t *testing.T) {
	if !HasCapability([]string{"embeddings"}, CapabilityEmbedding) {
		t.Fatal("alias embeddings must match embedding")
	}
	if HasCapability([]string{"chat"}, CapabilityVision) {
		t.Fatal("chat must not match vision")
	}
}

func TestInferCapabilities(t *testing.T) {
	cases := map[string][]string{
		"gpt-4o-mini":             {CapabilityChat, CapabilityVision},
		"gpt-4o":                  {CapabilityChat, CapabilityVision},
		"gpt-3.5-turbo":           {CapabilityChat},
		"whisper-1":               {CapabilityAudio},
		"tts-1":                   {CapabilitySpeech},
		"text-embedding-3-large":  {CapabilityEmbedding},
		"bge-reranker-v2":         {CapabilityRerank},
		"deepseek-reasoner":       {CapabilityChat, CapabilityReasoning},
		"gemini-2.5-pro":          {CapabilityChat, CapabilityVision},
		"":                        {CapabilityChat},
		"some-unknown-model-name": {CapabilityChat},
	}
	for model, want := range cases {
		if got := InferCapabilities(model); !reflect.DeepEqual(got, want) {
			t.Fatalf("InferCapabilities(%q) = %v, want %v", model, got, want)
		}
	}
}

func TestKnownCapability(t *testing.T) {
	for _, value := range CapabilityVocabulary() {
		if !KnownCapability(value) {
			t.Fatalf("%s must be known", value)
		}
	}
	if KnownCapability("teleportation") {
		t.Fatal("unknown capability must not be known")
	}
}
