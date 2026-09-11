// Package params applies Project parameter compatibility policy before routing.
package params

import (
	"encoding/json"
	"fmt"

	"github.com/F31/liteAIG/internal/kernel/interaction"
)

type UnsupportedParameterError struct{ Name string }

func (e *UnsupportedParameterError) Error() string {
	return fmt.Sprintf("UNSUPPORTED_PARAMETER: %s", e.Name)
}

func Filter(input map[string]json.RawMessage, support interaction.ParameterSupport, mode interaction.ParameterMode) (map[string]json.RawMessage, []interaction.ParameterWarning, error) {
	output := make(map[string]json.RawMessage)
	var warnings []interaction.ParameterWarning
	for name, value := range input {
		if support.Supported[name] || support.PassthroughAllowlist[name] {
			output[name] = append(json.RawMessage(nil), value...)
			continue
		}
		if mode == interaction.ParametersStrict {
			return nil, nil, &UnsupportedParameterError{Name: name}
		}
		warnings = append(warnings, interaction.ParameterWarning{Name: name, Code: "parameter_dropped"})
	}
	return output, warnings, nil
}
