// Gateway-side signed RuntimeBundle delivery (Stage 11): a split mode=gateway
// process with no local registry pulls the signed bundle from the Control Plane,
// runs Prepare (schema/checksum/signature), and atomically activates it. A NACK
// leaves the currently-loaded (LKG or prior) runtime untouched.
package app

import (
	"context"
	"crypto/ed25519"
	"errors"
	"log"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/platform/bundle"
)

// bundleSyncer pulls signed bundles from a split-mode Control Plane and
// activates them into the local registry. It also performs the cold-start full
// fetch for a mode=gateway process that has no Local-Store config yet.
type bundleSyncer struct {
	client    *bundle.Client
	publicKey ed25519.PublicKey
	registry  *runtime.ActiveRegistry
	clock     contracts.Clock
	poll      time.Duration
}

// newBundleSyncer builds the gateway puller. A nil public key disables bundle
// delivery entirely (monolithic mode).
func newBundleSyncer(client *bundle.Client, publicKey ed25519.PublicKey, registry *runtime.ActiveRegistry, clock contracts.Clock, poll time.Duration) *bundleSyncer {
	if poll <= 0 {
		poll = 30 * time.Second
	}
	return &bundleSyncer{client: client, publicKey: publicKey, registry: registry, clock: clock, poll: poll}
}

// validateForTenant mirrors the Control Plane's local reference validation on
// the Data Plane: the bundle must carry a compiled snapshot. Signature and
// checksum are already checked by Prepare.
func validateForTenant(b *bundle.RuntimeBundle) error {
	if b == nil || b.Snapshot == nil {
		return errors.New("bundle has no compiled snapshot")
	}
	return nil
}

// syncOnce fetches the latest bundles for every tenant the Control Plane serves
// and activates them atomically. NACKed bundles are skipped (the local runtime
// stays as-is), matching the spec "NACK keeps LKG".
func (s *bundleSyncer) syncOnce(ctx context.Context) error {
	if s == nil || s.client == nil || s.publicKey == nil {
		return nil
	}
	refs, err := s.client.ListTenantRefs(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, ref := range refs {
		if err := s.pullTenant(ctx, ref); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// pullTenant fetches, verifies, and activates one tenant's bundle.
func (s *bundleSyncer) pullTenant(ctx context.Context, ref string) error {
	pulled, err := s.client.Fetch(ctx, ref)
	if err != nil {
		return err
	}
	result := bundle.Prepare(pulled, s.publicKey, validateForTenant)
	if !result.Ack {
		log.Printf("gateway: bundle NACK for %s (reason %s); keeping current runtime", ref, result.Reason)
		return nil
	}
	if err := bundle.Activate(pulled, true); err != nil {
		return err
	}
	s.registry.ActivateTenant(ref, pulled.Snapshot)
	log.Printf("gateway: activated tenant %s from signed bundle v%d", ref, pulled.ConfigVersion)
	return nil
}

// run polls the Control Plane until ctx is cancelled.
func (s *bundleSyncer) run(ctx context.Context) {
	if s == nil || s.client == nil {
		return
	}
	// Cold start: a full fetch before serving traffic.
	if err := s.syncOnce(ctx); err != nil {
		log.Printf("gateway: cold-start bundle sync failed: %v", err)
	}
	ticker := time.NewTicker(s.poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.syncOnce(ctx); err != nil {
				// Fail-open: an unreachable Control Plane must not tear down an
				// already-loaded runtime. The next tick retries.
				log.Printf("gateway: bundle sync failed: %v", err)
			}
		}
	}
}
