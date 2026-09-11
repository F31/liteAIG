package app

import "testing"

func TestParseMode(t *testing.T) {
	for _, value := range []string{"all", "gateway", "control"} {
		mode, err := ParseMode(value)
		if err != nil {
			t.Fatalf("ParseMode(%q) error = %v", value, err)
		}
		if string(mode) != value {
			t.Fatalf("ParseMode(%q) = %q", value, mode)
		}
	}
}

func TestCompose(t *testing.T) {
	tests := []struct {
		mode             Mode
		gateway, control bool
	}{
		{mode: ModeAll, gateway: true, control: true},
		{mode: ModeGateway, gateway: true, control: false},
		{mode: ModeControl, gateway: false, control: true},
	}

	for _, tt := range tests {
		plan := Compose(tt.mode)
		if plan.Gateway != tt.gateway || plan.Control != tt.control {
			t.Fatalf("Compose(%q) = %+v", tt.mode, plan)
		}
	}
}
