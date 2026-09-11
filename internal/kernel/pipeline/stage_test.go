package pipeline

import "testing"

func TestFixedOrderReturnsCopy(t *testing.T) {
	order := Order()
	order[0] = AccountingTelemetry

	fresh := Order()
	if fresh[0] != Admission {
		t.Fatalf("Order() was mutated: first stage = %s", fresh[0])
	}
	if fresh[6] != AccountingTelemetry {
		t.Fatalf("Order() final stage = %s", fresh[6])
	}
}
