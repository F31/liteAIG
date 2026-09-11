// Package retry defines bounded retry timing independent from execution orchestration.
package retry

import (
	"context"
	"time"
)

type Policy struct {
	MaxAttemptsPerDeployment, MaxTotalCalls                                  int
	AttemptTimeout, TotalTimeout, StreamIdleTimeout, BaseBackoff, MaxBackoff time.Duration
}

func DefaultPolicy() Policy {
	return Policy{MaxAttemptsPerDeployment: 2, MaxTotalCalls: 4, AttemptTimeout: 30 * time.Second, TotalTimeout: 60 * time.Second, StreamIdleTimeout: 15 * time.Second, BaseBackoff: 200 * time.Millisecond, MaxBackoff: 4 * time.Second}
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
