// Package retry defines bounded retry timing independent from execution orchestration.
package retry

import (
	"context"
	"time"
)

type Policy struct {
	MaxAttemptsPerDeployment, MaxTotalCalls int
	AttemptTimeout, TotalTimeout            time.Duration
	// StreamTotalTimeout bounds a whole streaming response. Zero (the default)
	// means no overall deadline: StreamIdleTimeout and FirstTokenTimeout still
	// bound stalls, but a long healthy stream is never cut by a fixed budget.
	StreamTotalTimeout time.Duration
	StreamIdleTimeout  time.Duration
	// FirstTokenTimeout bounds how long a streaming attempt may take to emit
	// its first byte before the gateway fails over to the next deployment.
	// Until the first byte is committed a failover is invisible to the client
	// (no replay / no duplicated output). Zero falls back to StreamIdleTimeout.
	FirstTokenTimeout time.Duration
	// ProviderTimeout bounds a non-streaming upstream HTTP call. Streaming
	// calls deliberately do not apply it: the executor's first-token and idle
	// timers bound the stream instead.
	ProviderTimeout         time.Duration
	BaseBackoff, MaxBackoff time.Duration
}

// DefaultPolicy is tuned for AI traffic: non-streaming calls may legitimately
// take minutes, while streams are governed by first-token and idle deadlines
// rather than a hard total timeout.
func DefaultPolicy() Policy {
	return Policy{
		MaxAttemptsPerDeployment: 2,
		MaxTotalCalls:            4,
		AttemptTimeout:           120 * time.Second,
		TotalTimeout:             300 * time.Second,
		StreamTotalTimeout:       0,
		StreamIdleTimeout:        60 * time.Second,
		FirstTokenTimeout:        30 * time.Second,
		ProviderTimeout:          120 * time.Second,
		BaseBackoff:              200 * time.Millisecond,
		MaxBackoff:               4 * time.Second,
	}
}
func (p Policy) Backoff(retryNumber int, jitter time.Duration) time.Duration {
	value := p.BaseBackoff
	for i := 1; i < retryNumber; i++ {
		value *= 2
		if value >= p.MaxBackoff {
			value = p.MaxBackoff
			break
		}
	}
	value += jitter
	if value > p.MaxBackoff {
		value = p.MaxBackoff
	}
	return value
}

type Sleeper interface {
	Sleep(context.Context, time.Duration) error
}
type TimerSleeper struct{}

func (TimerSleeper) Sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
