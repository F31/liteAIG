package apikey

import (
	"bytes"
	"strings"
	"testing"
)

func TestGenerateAndParseVirtualKey(t *testing.T) {
	generator := NewGenerator(bytes.NewReader(make([]byte, 48)))
	tenantRef := "11111111111111111111111111111111"
	token, err := generator.NewKey(tenantRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(token.PublicID) != 16 || len(token.Secret) != 48 {
		t.Fatalf("token entropy lengths = %+v", token)
	}
	parsed, err := Parse(token.String())
	if err != nil || parsed != token {
		t.Fatalf("Parse() = %+v, %v", parsed, err)
	}
}

func TestParseAcceptsLegacyLongKey(t *testing.T) {
	legacy := "sk-lia-v1_" +
		"11111111111111111111111111111111_" +
		"22222222222222222222222222222222_" +
		strings.Repeat("3", 64)
	parsed, err := Parse(legacy)
	if err != nil {
		t.Fatalf("Parse() legacy key = %v", err)
	}
	if len(parsed.PublicID) != 32 || len(parsed.Secret) != 64 {
		t.Fatalf("legacy token = %+v", parsed)
	}
}

func TestParseRejectsMalformedKey(t *testing.T) {
	for _, value := range []string{
		"sk-lia-v1_bad",
		"sk-lia-v2_11111111111111111111111111111111_2222222222222222_33333333333333333333333333333333333333333333333333333333",
		"sk-lia-v1_11111111111111111111111111111111_2222222222222222_33333333333333333333333333333333333333333333333333333333",
	} {
		if _, err := Parse(value); err == nil {
			t.Fatalf("Parse() accepted malformed key %q", value)
		}
	}
}
