package coordination

import (
	"context"
	"errors"
	"sync"
)

// BudgetConsistency enumerates cross-Region Budget modes.
type BudgetConsistency string

const (
	BudgetRegional   BudgetConsistency = "regional"
	BudgetGlobalSoft BudgetConsistency = "global_soft"
	BudgetGlobalHard BudgetConsistency = "global_hard"
)

// SliceConfig bounds a global_soft slice.
type SliceConfig struct {
	Region      string
	StrictLimit float64
	Overshoot   float64 // extra admitted beyond StrictLimit (e.g. 0.10)
}

// SliceState is one region's slice usage.
type SliceState struct {
	Region   string
	Used     float64
	Strict   float64
	Overshot bool
}

// ReconcileEvent reports a slice reconcile.
type ReconcileEvent struct {
	WindowKey string
	Region    string
	Adjusted  float64
}

// SliceAuthority owns per-Region budget slices over one window key.
type SliceAuthority struct {
	mu       sync.Mutex
	window   string
	regional bool // strict single-authority (regional or global_hard)
	slices   map[string]*sliceCounter
}

// ForMode returns the appropriate authority for a consistency mode: strict for
// regional/global_hard, slice-overshoot for global_soft.
func ForMode(window string, mode BudgetConsistency) *SliceAuthority {
	return NewSliceAuthority(window, mode != BudgetGlobalSoft)
}

type sliceCounter struct {
	strict float64
	used   float64
}

// NewSliceAuthority builds an authority over a window.
func NewSliceAuthority(window string, regional bool) *SliceAuthority {
	return &SliceAuthority{window: window, regional: regional, slices: map[string]*sliceCounter{}}
}

var ErrSliceOvershoot = errors.New("budget slice overshoot exceeds bound")

// Admit reserves amount on a region's slice under the consistency mode.
func (a *SliceAuthority) Admit(_ context.Context, region string, amount float64, strict float64, overshoot float64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	slice, ok := a.slices[region]
	if !ok {
		slice = &sliceCounter{strict: strict}
		a.slices[region] = slice
	}
	if a.regional {
		if slice.used+amount > slice.strict {
			return ErrSliceOvershoot
		}
		slice.used += amount
		return nil
	}
	// global_soft: bounded overshoot
	bound := slice.strict + overshoot
	if slice.used+amount > bound {
		return ErrSliceOvershoot
	}
	slice.used += amount
	return nil
}

// Reconcile adjusts a region's slice to the true window and returns an event.
func (a *SliceAuthority) Reconcile(_ context.Context, region string, trueUsed float64) (ReconcileEvent, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	slice, ok := a.slices[region]
	if !ok {
		return ReconcileEvent{}, errors.New("slice not found")
	}
	slice.used = trueUsed
	return ReconcileEvent{WindowKey: a.window, Region: region, Adjusted: trueUsed}, nil
}

// State reports a region's slice usage.
func (a *SliceAuthority) State(region string) (SliceState, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	slice, ok := a.slices[region]
	if !ok {
		return SliceState{}, false
	}
	return SliceState{Region: region, Used: slice.used, Strict: slice.strict, Overshot: slice.used > slice.strict}, true
}
