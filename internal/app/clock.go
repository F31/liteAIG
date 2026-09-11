package app

import "time"

// systemClock implements contracts.Clock with the real wall clock.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }
