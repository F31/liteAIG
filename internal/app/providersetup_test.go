package app

import (
	"context"
	"reflect"
	"testing"

	"github.com/F31/liteAIG/internal/catalog"
	"github.com/F31/liteAIG/internal/controlplane/config"
)

func TestMatchCatalogModelPrefersProvider(t *testing.T) {
	models := []catalog.ModelCatalogItem{
		{ID: "a", ProviderID: "deepseek", Model: "shared-model", Capabilities: []string{"chat"}},
		{ID: "b", ProviderID: "openai", Model: "shared-model", Capabilities: []string{"chat", "vision"}},
	}
	item, ok := matchCatalogModel(models, "openai", "shared-model")
	if !ok || item.ID != "b" {
		t.Fatalf("matchCatalogModel() = %+v ok=%v, want the openai entry", item, ok)
	}
	item, ok = matchCatalogModel(models, "unknown", "shared-model")
	if !ok || item.ID != "a" {
		t.Fatalf("fallback match = %+v ok=%v, want the first entry", item, ok)
	}
	if _, ok := matchCatalogModel(models, "openai", "missing"); ok {
		t.Fatal("a missing model must not match")
	}
}

// TestResolveModelProfileFallsBackToInference proves the catalog is optional:
// without it (or without an entry) the wizard still assigns sensible
// capabilities from the model name instead of hardcoding chat.
func TestResolveModelProfileFallsBackToInference(t *testing.T) {
	setup := &realProviderSetup{}
	caps, window := setup.resolveModelProfile(context.Background(), "openai", "whisper-1")
	if !reflect.DeepEqual(caps, []string{config.CapabilityAudio}) {
		t.Fatalf("capabilities = %v, want [audio]", caps)
	}
	if window != 8192 {
		t.Fatalf("context window = %d, want 8192", window)
	}
}

// TestCatalogCapabilitiesAreCanonicalized proves a catalog entry's raw
// capabilities are normalized to the canonical vocabulary (multimodal→vision).
func TestCatalogCapabilitiesAreCanonicalized(t *testing.T) {
	window := 128000
	item, ok := matchCatalogModel([]catalog.ModelCatalogItem{{
		ID: "x", ProviderID: "openai", Model: "gpt-4o-mini",
		Capabilities: []string{"chat", "multimodal"}, ContextWindow: &window,
	}}, "openai", "gpt-4o-mini")
	if !ok {
		t.Fatal("expected a catalog match")
	}
	caps := config.CanonicalCapabilities(item.Capabilities)
	want := []string{config.CapabilityChat, config.CapabilityVision}
	if !reflect.DeepEqual(caps, want) {
		t.Fatalf("capabilities = %v, want %v", caps, want)
	}
}
