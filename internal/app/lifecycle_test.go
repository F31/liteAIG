package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidateDrainConfig(t *testing.T) {
	base := DrainConfig{DrainTimeout: 30 * time.Second, StreamDrainTimeout: 60 * time.Second, ForceShutdownTimeout: 70 * time.Second}
	if err := ValidateDrainConfig(base); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	bad := base
	bad.DrainTimeout = 90 * time.Second
	if err := ValidateDrainConfig(bad); err == nil {
		t.Fatal("drain_timeout > force_shutdown_timeout accepted")
	}
	bad = base
	bad.StreamDrainTimeout = 80 * time.Second
	if err := ValidateDrainConfig(bad); err == nil {
		t.Fatal("stream_drain_timeout > force_shutdown_timeout accepted")
	}
	negative := base
	negative.DrainTimeout = -time.Second
	if err := ValidateDrainConfig(negative); err == nil {
		t.Fatal("negative drain timeout accepted")
	}
	def := (DrainConfig{}).WithDefaults()
	if def.DrainTimeout != DefaultDrainTimeout || def.ForceShutdownTimeout != DefaultForceShutdownTimeout {
		t.Fatalf("defaults = %+v", def)
	}
}

func TestLifecycleReadyFlipsOnDrain(t *testing.T) {
	lifecycle, err := NewLifecycle(DrainConfig{}, NewStreamRegistry(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !lifecycle.Ready() || !lifecycle.Running() {
		t.Fatal("expected ready at start")
	}
	if err := lifecycle.BeginDrain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lifecycle.Ready() {
		t.Fatal("expected not-ready while draining")
	}
	// Idempotent: second BeginDrain is a no-op.
	if err := lifecycle.BeginDrain(context.Background()); err != nil {
		t.Fatalf("second BeginDrain = %v", err)
	}
}

func TestLifecycleWaitsForInflight(t *testing.T) {
	lifecycle, err := NewLifecycle(DrainConfig{DrainTimeout: time.Second}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.AddInflight()
	done := make(chan error, 1)
	go func() { done <- lifecycle.BeginDrain(context.Background()) }()
	select {
	case <-done:
		t.Fatal("BeginDrain returned before in-flight completed")
	case <-time.After(50 * time.Millisecond):
	}
	lifecycle.DoneInflight()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("BeginDrain did not return after in-flight completed")
	}
}

func TestLifecycleRefusesNewStreamsAndWaits(t *testing.T) {
	registry := NewStreamRegistry()
	lifecycle, err := NewLifecycle(DrainConfig{DrainTimeout: 200 * time.Millisecond, StreamDrainTimeout: 200 * time.Millisecond, ForceShutdownTimeout: 500 * time.Millisecond}, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := registry.Register()
	if !ok {
		t.Fatal("could not register stream before drain")
	}
	done := make(chan error, 1)
	go func() { done <- lifecycle.BeginDrain(context.Background()) }()
	// While draining, new long streams are refused.
	time.Sleep(20 * time.Millisecond)
	if _, ok := registry.Register(); ok {
		t.Fatal("new stream accepted while draining")
	}
	registry.Unregister(id)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("BeginDrain did not return after stream completed")
	}
}

func TestLifecycleForceShutdownCapsDrain(t *testing.T) {
	lifecycle, err := NewLifecycle(DrainConfig{DrainTimeout: 150 * time.Millisecond, StreamDrainTimeout: 150 * time.Millisecond, ForceShutdownTimeout: 150 * time.Millisecond}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.AddInflight() // never completes
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = lifecycle.BeginDrain(ctx)
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected force-shutdown deadline error, got %v", err)
	}
	if elapsed > 400*time.Millisecond {
		t.Fatalf("drain exceeded force-shutdown cap: %v", elapsed)
	}
	if lifecycle.Ready() {
		t.Fatal("expected exited")
	}
}

func TestLifecycleOnDrainFinalizes(t *testing.T) {
	var finalized atomic.Int32
	var wg sync.WaitGroup
	lifecycle, err := NewLifecycle(DrainConfig{}, nil, func(context.Context) error {
		finalized.Add(1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wg.Add(1)
	go func() { defer wg.Done(); _ = lifecycle.BeginDrain(context.Background()) }()
	wg.Wait()
	if finalized.Load() != 1 {
		t.Fatalf("onDrain calls = %d", finalized.Load())
	}
}

func TestLifecycleDrainWaitsForInflightFinalization(t *testing.T) {
	var released atomic.Int32
	lifecycle, err := NewLifecycle(DrainConfig{DrainTimeout: time.Second, StreamDrainTimeout: time.Second, ForceShutdownTimeout: 2 * time.Second}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.AddInflight()
	go func() {
		// Simulate an in-flight request finishing: it reconciles budget and
		// lease before marking itself complete.
		time.Sleep(50 * time.Millisecond)
		released.Add(1)
		lifecycle.DoneInflight()
	}()
	if err := lifecycle.BeginDrain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if released.Load() != 1 {
		t.Fatal("budget/lease finalization did not complete before process exit")
	}
	if lifecycle.Ready() {
		t.Fatal("lifecycle still ready after drain")
	}
}
