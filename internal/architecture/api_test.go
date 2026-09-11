package architecture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExportedFileSymbols(t *testing.T) {
	dir := t.TempDir()
	source := `package client

import "fmt"

type Client struct{}

type Alias = map[string]string

const DefaultVersion = "1"

var ErrClosed = fmt.Errorf("closed")

func New() *Client { return nil }

func (c *Client) Send() error { return nil }

func (c Client) SendStream() error { return nil }

func unexported() {}
`
	path := filepath.Join(dir, "client.go")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	symbols, err := exportedFileSymbols("example.com/mod/pkg/client", path)
	if err != nil {
		t.Fatal(err)
	}
	want := []ExportedSymbol{
		{Package: "example.com/mod/pkg/client", Kind: "type", Name: "Client"},
		{Package: "example.com/mod/pkg/client", Kind: "type", Name: "Alias"},
		{Package: "example.com/mod/pkg/client", Kind: "const", Name: "DefaultVersion"},
		{Package: "example.com/mod/pkg/client", Kind: "var", Name: "ErrClosed"},
		{Package: "example.com/mod/pkg/client", Kind: "func", Name: "New"},
		{Package: "example.com/mod/pkg/client", Kind: "method", Method: "Client", Name: "Send"},
		{Package: "example.com/mod/pkg/client", Kind: "method", Method: "Client", Name: "SendStream"},
	}
	if len(symbols) != len(want) {
		t.Fatalf("symbols = %d: %+v", len(symbols), symbols)
	}
	SortSymbols(symbols)
	SortSymbols(want)
	for i := range want {
		if symbols[i] != want[i] {
			t.Fatalf("symbol[%d] = %+v want %+v", i, symbols[i], want[i])
		}
	}
}

func TestCompareSurfaceDetectsRemovalOnly(t *testing.T) {
	before := []ExportedSymbol{
		{Package: "pkg/a", Kind: "func", Name: "Keep"},
		{Package: "pkg/a", Kind: "func", Name: "Removed"},
	}
	after := []ExportedSymbol{
		{Package: "pkg/a", Kind: "func", Name: "Keep"},
		{Package: "pkg/a", Kind: "func", Name: "Added"},
	}
	violations := CompareSurface(before, after)
	if len(violations) != 1 {
		t.Fatalf("violations = %+v", violations)
	}
	if violations[0].Rule != "module-api-breaking-removal" || !contains(violations[0].Detail, "Removed") {
		t.Fatalf("violation = %+v", violations[0])
	}
}

func TestExportedSnapshotRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exported.json")
	symbols := []ExportedSymbol{
		{Package: "pkg/a", Kind: "type", Name: "Beta"},
		{Package: "pkg/a", Kind: "func", Name: "Alpha"},
	}
	if err := SaveExportedSnapshot(path, symbols, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadExportedSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 || loaded[0].Name != "Alpha" || loaded[1].Name != "Beta" {
		t.Fatalf("loaded = %+v", loaded)
	}
}

func contains(value, sub string) bool {
	return len(value) >= len(sub) && (func() bool {
		for i := 0; i <= len(value)-len(sub); i++ {
			if value[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
