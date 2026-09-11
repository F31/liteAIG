package params

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

func TestFilterStrictAndPermissive(t *testing.T) {
	input := map[string]json.RawMessage{"seed": json.RawMessage("42"), "unsafe": json.RawMessage("true")}
	support := interaction.ParameterSupport{Supported: map[string]bool{"seed": true}}
	if _, _, err := Filter(input, support, interaction.ParametersStrict); err == nil {
		t.Fatal("strict mode accepted unsupported parameter")
	} else {
		var unsupported *UnsupportedParameterError
		if !errors.As(err, &unsupported) || unsupported.Name != "unsafe" {
			t.Fatalf("error=%v", err)
		}
	}
	output, warnings, err := Filter(input, support, interaction.ParametersPermissive)
	if err != nil || len(output) != 1 || len(warnings) != 1 {
		t.Fatalf("Filter()=%+v,%+v,%v", output, warnings, err)
	}
}

func TestFilterAllowsExplicitPassthrough(t *testing.T) {
	input := map[string]json.RawMessage{"vendor_option": json.RawMessage(`"enabled"`)}
	support := interaction.ParameterSupport{PassthroughAllowlist: map[string]bool{"vendor_option": true}}
	output, warnings, err := Filter(input, support, interaction.ParametersStrict)
	if err != nil || len(warnings) != 0 || string(output["vendor_option"]) != `"enabled"` {
		t.Fatalf("Filter()=%+v,%+v,%v", output, warnings, err)
	}
}
