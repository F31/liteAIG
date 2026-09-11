// Package pipeline defines the fixed Data Plane execution stages.
package pipeline

// Stage identifies one position in the compile-time-fixed pipeline.
type Stage uint8

const (
	Admission Stage = iota + 1
	InputGuardrail
	PolicyCostPreflight
	Resolution
	ExecutionResilience
	OutputStreamGuardrail
	AccountingTelemetry
)

var fixedOrder = [...]Stage{
	Admission,
	InputGuardrail,
	PolicyCostPreflight,
	Resolution,
	ExecutionResilience,
	OutputStreamGuardrail,
	AccountingTelemetry,
}

// Order returns a copy so callers cannot mutate the pipeline definition.
func Order() [7]Stage { return fixedOrder }

func (s Stage) String() string {
	switch s {
	case Admission:
		return "admission"
	case InputGuardrail:
		return "input_guardrail"
	case PolicyCostPreflight:
		return "policy_cost_preflight"
	case Resolution:
		return "resolution"
	case ExecutionResilience:
		return "execution_resilience"
	case OutputStreamGuardrail:
		return "output_stream_guardrail"
	case AccountingTelemetry:
		return "accounting_telemetry"
	default:
		return "unknown"
	}
}
