// Package secrets defines a replaceable Secret Provider and a caching wrapper
// so that KMS/Vault is never a synchronous per-request hot-path dependency.
package secrets

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"
)

// Provider resolves an opaque secret reference to material server-side.
type Provider interface {
	Resolve(context.Context, string) ([]byte, error)
}

// ErrNotFound reports an unresolvable secret reference.
var ErrNotFound = errors.New("secret not found")

// MemoryProvider resolves references from a static map (tests, Lite, or
// references that carry no external secret dependency).
type MemoryProvider struct{ values map[string][]byte }

// NewMemoryProvider builds a provider over a static reference map.
func NewMemoryProvider(values map[string][]byte) *MemoryProvider {
	return &MemoryProvider{values: values}
}

// Resolve returns the material for a reference.
func (m *MemoryProvider) Resolve(_ context.Context, ref string) ([]byte, error) {
	value, ok := m.values[ref]
	if !ok {
		return nil, ErrNotFound
	}
	return value, nil
}

// EnvProvider resolves `secret://env/<NAME>` references from the environment so
// Standard deployments can run without a KMS while keeping refs opaque.
type EnvProvider struct{ prefix string }

// NewEnvProvider builds a provider that maps `secret://env/NAME` to the
// environment variable NAME.
func NewEnvProvider() *EnvProvider { return &EnvProvider{prefix: "secret://env/"} }

// Resolve reads the referenced environment variable.
func (p *EnvProvider) Resolve(_ context.Context, ref string) ([]byte, error) {
	name, ok := strings.CutPrefix(ref, p.prefix)
	if !ok || name == "" {
		return nil, ErrNotFound
	}
	value, ok := os.LookupEnv(name)
	if !ok {
		return nil, ErrNotFound
	}
	return []byte(value), nil
}

// CacheConfig governs in-memory credential caching.
type CacheConfig struct {
	TTL             time.Duration
	StaleGrace      time.Duration
	RotationOverlap time.Duration
}

// WithDefaults fills zero-valued cache settings.
func (c CacheConfig) WithDefaults() CacheConfig {
	if c.TTL <= 0 {
		c.TTL = 5 * time.Minute
	}
	if c.StaleGrace <= 0 {
		c.StaleGrace = time.Minute
	}
	return c
}

// ValidateCacheConfig rejects invalid cache settings.
func ValidateCacheConfig(c CacheConfig) error {
	if c.TTL < 0 || c.StaleGrace < 0 || c.RotationOverlap < 0 {
		return errors.New("secret cache durations must be non-negative")
	}
	return nil
}

type cacheEntry struct {
	value     []byte
	fetchedAt time.Time
	degraded  bool
}

// CachingProvider caches resolved credentials with TTL and stale-grace so that
// a temporary Secret Provider outage continues to serve already-decrypted
// credentials, and fails closed after grace.
type CachingProvider struct {
	provider Provider
	config   CacheConfig
	onOutage func(ref string, degraded bool)
	now      func() time.Time

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

// NewCachingProvider wraps a Provider with an in-memory cache.
func NewCachingProvider(provider Provider, config CacheConfig, onOutage func(ref string, degraded bool)) (*CachingProvider, error) {
	if provider == nil {
		return nil, errors.New("secret provider is required")
	}
	if err := ValidateCacheConfig(config); err != nil {
		return nil, err
	}
	config = config.WithDefaults()
	return &CachingProvider{provider: provider, config: config, onOutage: onOutage, now: time.Now, cache: map[string]cacheEntry{}}, nil
}

// Resolve returns credential material, preferring a fresh cache entry and
// serving a stale entry within grace when the provider is unavailable.
func (c *CachingProvider) Resolve(ctx context.Context, ref string) ([]byte, error) {
	now := c.now()
	c.mu.RLock()
	entry, cached := c.cache[ref]
	c.mu.RUnlock()
	if cached && now.Sub(entry.fetchedAt) <= c.config.TTL {
		return entry.value, nil
	}

	value, err := c.provider.Resolve(ctx, ref)
	if err == nil {
		c.mu.Lock()
		c.cache[ref] = cacheEntry{value: value, fetchedAt: now}
		c.mu.Unlock()
		return value, nil
	}

	if cached && now.Sub(entry.fetchedAt) <= c.config.TTL+c.config.StaleGrace {
		c.mu.Lock()
		c.cache[ref] = cacheEntry{value: entry.value, fetchedAt: entry.fetchedAt, degraded: true}
		c.mu.Unlock()
		if c.onOutage != nil {
			c.onOutage(ref, true)
		}
		return entry.value, nil
	}

	if c.onOutage != nil {
		c.onOutage(ref, false)
	}
	return nil, err
}

// Degraded reports whether the most recent resolution for ref served a stale
// credential during a provider outage.
func (c *CachingProvider) Degraded(ref string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache[ref].degraded
}

// CacheSize returns the number of cached references (diagnostics).
func (c *CachingProvider) CacheSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}

// Provider exposes the underlying provider for testing.
func (c *CachingProvider) Provider() Provider { return c.provider }

// SetNowForTest overrides the clock used for cache TTL/grace decisions.
func (c *CachingProvider) SetNowForTest(now func() time.Time) { c.now = now }

// RevokeAll removes all material to simulate a total Secret Provider outage.
func (m *MemoryProvider) RevokeAll() { m.values = map[string][]byte{} }
