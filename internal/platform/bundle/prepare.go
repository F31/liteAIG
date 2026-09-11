// Package bundle owns the signed immutable RuntimeBundle delivered to the Data
// Plane.
package bundle

import (
	"crypto/ed25519"
	"errors"
)

// Result is the outcome of Prepare.
type Result struct {
	Ack    bool
	Reason string
}

// LocalValidator checks a bundle's references and local resource constraints.
type LocalValidator func(*RuntimeBundle) error

// Prepare validates a received bundle against the schema version, signature,
// checksum, and local resources. It returns ACK or a NACK reason. A single node
// NACK must never load a partial bundle.
func Prepare(b *RuntimeBundle, publicKey ed25519.PublicKey, validator LocalValidator) Result {
	if b.SchemaVersion == "" || b.TenantID == "" || b.TenantRef == "" {
		return Result{Ack: false, Reason: "incomplete bundle metadata"}
	}
	if err := b.Verify(publicKey); err != nil {
		return Result{Ack: false, Reason: err.Error()}
	}
	if validator != nil {
		if err := validator(b); err != nil {
			return Result{Ack: false, Reason: err.Error()}
		}
	}
	return Result{Ack: true}
}

// ActivationError reports a bundle that cannot activate atomically.
var ActivationError = errors.New("cannot activate a bundle that was not ACKed")

// Activate replaces the active snapshot only for an ACKed bundle. A NACKed
// bundle must be rejected by the caller before reaching Activate; this guard
// prevents partial activation.
func Activate(b *RuntimeBundle, acked bool) error {
	if !acked {
		return ActivationError
	}
	if b.Snapshot == nil {
		return errors.New("bundle has no compiled snapshot")
	}
	return nil
}
