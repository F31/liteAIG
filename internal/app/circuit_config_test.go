package app

import (
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/resilience/circuit"
)

func TestBreakerConfigFromSnapshotEmptyKeepsDefaults(t *testing.T) {
	def := circuit.DefaultConfig()
	got := breakerConfigFromSnapshot(runtime.CircuitConfig{})
	if got.MinSamples != def.MinSamples || got.ErrorRate != def.ErrorRate ||
		got.Cooldown != def.Cooldown || got.CooldownFactor != def.CooldownFactor || got.MaxCooldown != def.MaxCooldown {
		t.Fatalf("empty snapshot circuit = %+v, want defaults %+v", got, def)
	}
}

func TestBreakerConfigFromSnapshotMapsPublishedValues(t *testing.T) {
	got := breakerConfigFromSnapshot(runtime.CircuitConfig{
		MinSamples:        5,
		ErrorRate:         0.25,
		InitialCooldownMS: 1500,
		MaxCooldownMS:     30000,
		CooldownFactor:    3,
	})
	if got.MinSamples != 5 || got.ErrorRate != 0.25 || got.CooldownFactor != 3 {
		t.Fatalf("mapped = %+v", got)
	}
	if got.Cooldown != 1500*time.Millisecond {
		t.Fatalf("cooldown = %v, want 1.5s", got.Cooldown)
	}
	if got.MaxCooldown != 30000*time.Millisecond {
		t.Fatalf("max cooldown = %v, want 30s", got.MaxCooldown)
	}
}

func TestBreakerConfigFromSnapshotFallsBackForZeroDurations(t *testing.T) {
	def := circuit.DefaultConfig()
	got := breakerConfigFromSnapshot(runtime.CircuitConfig{MinSamples: 5, ErrorRate: 0.25})
	if got.MinSamples != 5 || got.ErrorRate != 0.25 {
		t.Fatalf("mapped = %+v", got)
	}
	if got.Cooldown != def.Cooldown || got.MaxCooldown != def.Cooldown {
		t.Fatalf("durations = %+v, want fallback %+v", got, def)
	}
}
