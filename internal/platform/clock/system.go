// Package clock provides production and testable time sources.
package clock

import "time"

type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }
