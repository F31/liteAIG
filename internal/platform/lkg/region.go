// Package lkg persists the Data Plane's Last Known Good RuntimeBundles on local
// disk, including a Region-scoped layout and DR readiness coupling.
package lkg

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
)

// RegionStore wraps a Store per Region so one Region can boot its own LKG
// independently and a corrupt bundle in one Region never affects another.
type RegionStore struct {
	mu     sync.Mutex
	root   string
	stores map[string]*Store
	verify Verifier
}

// NewRegionStore builds a region-scoped LKG store rooted at dir. Layout:
// <dir>/regions/<region>/<tenant>/active.bundle.
func NewRegionStore(dir string, verifier Verifier) (*RegionStore, error) {
	if dir == "" {
		return nil, errors.New("region LKG directory is required")
	}
	if verifier == nil {
		return nil, errors.New("region LKG bundle verifier is required")
	}
	return &RegionStore{root: dir, stores: map[string]*Store{}, verify: verifier}, nil
}

func (r *RegionStore) storeFor(region string) (*Store, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if store, ok := r.stores[region]; ok {
		return store, nil
	}
	store, err := New(filepath.Join(r.root, "regions", region))
	if err != nil {
		return nil, err
	}
	r.stores[region] = store
	return store, nil
}

// Save writes a bundle into a Region's LKG.
func (r *RegionStore) Save(ctx context.Context, region, tenantRef string, bundle Bundle) error {
	store, err := r.storeFor(region)
	if err != nil {
		return err
	}
	return store.Save(ctx, tenantRef, bundle)
}

// Ready reports whether a Region has a loadable active or previous bundle for
// a tenant.
func (r *RegionStore) Ready(ctx context.Context, region, tenantRef string) bool {
	store, err := r.storeFor(region)
	if err != nil {
		return false
	}
	_, err = store.LoadVerified(ctx, tenantRef, r.verify)
	return err == nil
}

// RegionReady couples node readiness with the Region's tenant bundle.
func (r *RegionStore) RegionReady(ctx context.Context, region, tenantRef string, nodeReady bool) bool {
	return nodeReady && r.Ready(ctx, region, tenantRef)
}
