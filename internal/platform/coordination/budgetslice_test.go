package coordination

import (
	"context"
	"errors"
	"testing"
)

func TestRegionalStrict(t *testing.T) {
	ctx := context.Background()
	authority := NewSliceAuthority("w", true) // regional strict
	if err := authority.Admit(ctx, "eu", 5, 10, 0); err != nil {
		t.Fatal(err)
	}
	if err := authority.Admit(ctx, "eu", 6, 10, 0); !errors.Is(err, ErrSliceOvershoot) {
		t.Fatalf("regional overshoot error = %v", err)
	}
}

func TestGlobalSoftBoundedOvershootAndReconcile(t *testing.T) {
	ctx := context.Background()
	authority := NewSliceAuthority("w", false) // global_soft
	// strict 10, overshoot 2 → admits up to 12.
	if err := authority.Admit(ctx, "eu", 11, 10, 2); err != nil {
		t.Fatalf("within-overshoot admit = %v", err)
	}
	// Beyond the bound (12) is rejected.
	if err := authority.Admit(ctx, "eu", 2, 10, 2); !errors.Is(err, ErrSliceOvershoot) {
		t.Fatalf("overshoot beyond bound error = %v", err)
	}
	// Reconcile to the true window and emit an event.
	event, err := authority.Reconcile(ctx, "eu", 9)
	if err != nil || event.Region != "eu" || event.Adjusted != 9 {
		t.Fatalf("reconcile = %+v, %v", event, err)
	}
	// Idempotent reconcile.
	if _, err := authority.Reconcile(ctx, "eu", 9); err != nil {
		t.Fatalf("idempotent reconcile = %v", err)
	}
}

func TestGlobalHardIsStrict(t *testing.T) {
	ctx := context.Background()
	authority := NewSliceAuthority("w", true) // global_hard uses a single strict authority
	if err := authority.Admit(ctx, "us", 5, 10, 0); err != nil {
		t.Fatal(err)
	}
	if err := authority.Admit(ctx, "us", 6, 10, 0); !errors.Is(err, ErrSliceOvershoot) {
		t.Fatalf("global_hard overshoot error = %v", err)
	}
}

func TestForModeSelectsAuthority(t *testing.T) {
	ctx := context.Background()
	// global_soft → overshoot allowed.
	soft := ForMode("w", BudgetGlobalSoft)
	if err := soft.Admit(ctx, "eu", 11, 10, 2); err != nil {
		t.Fatalf("global_soft overshoot rejected: %v", err)
	}
	// regional → strict.
	regional := ForMode("w", BudgetRegional)
	if err := regional.Admit(ctx, "eu", 11, 10, 0); !errors.Is(err, ErrSliceOvershoot) {
		t.Fatalf("regional overshoot error = %v", err)
	}
	// global_hard → strict.
	hard := ForMode("w", BudgetGlobalHard)
	if err := hard.Admit(ctx, "eu", 11, 10, 0); !errors.Is(err, ErrSliceOvershoot) {
		t.Fatalf("global_hard overshoot error = %v", err)
	}
}
