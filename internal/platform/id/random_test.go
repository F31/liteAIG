package id

import (
	"bytes"
	"testing"
)

func TestGeneratorCreatesUUIDv4Shape(t *testing.T) {
	generator := NewGenerator(bytes.NewReader(make([]byte, 16)))
	got, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	if got != "00000000-0000-4000-8000-000000000000" {
		t.Fatalf("New() = %q", got)
	}
}

func TestGeneratorCreates128BitTenantRef(t *testing.T) {
	generator := NewGenerator(bytes.NewReader(make([]byte, 16)))
	got, err := generator.NewTenantRef()
	if err != nil || len(got) != 32 {
		t.Fatalf("NewTenantRef() = %q, %v", got, err)
	}
}
