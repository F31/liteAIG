// Package contracts defines stable ports implemented outside the Kernel.
package contracts

import "time"

// Clock makes time-dependent policy and lifecycle behavior deterministic in tests.
type Clock interface {
	Now() time.Time
}

// IDGenerator creates opaque request and event identifiers.
type IDGenerator interface {
	New() (string, error)
}
