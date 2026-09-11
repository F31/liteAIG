package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSemanticDiffRedactsSecretReferences(t *testing.T) {
	before := validConfig()
	after := validConfig()
	before.Credentials[0].SecretRef = "secret://tenant/old-sensitive-value"
	after.Credentials[0].SecretRef = "secret://tenant/new-sensitive-value"
	after.LogicalModels[0].Alias = "renamed-chat"

	diff, err := SemanticDiff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(diff)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "old-sensitive-value") || strings.Contains(text, "new-sensitive-value") {
		t.Fatalf("SemanticDiff leaked secret refs: %s", text)
	}
	if !strings.Contains(text, "[redacted]") || !strings.Contains(text, "renamed-chat") {
		t.Fatalf("SemanticDiff output = %s", text)
	}
}
