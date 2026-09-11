package secrets

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func controllableProvider(ok *atomic.Bool, value []byte) Provider {
	return ProviderFunc(func(context.Context, string) ([]byte, error) {
		if ok.Load() {
			return value, nil
		}
		return nil, ErrNotFound
	})
}

type ProviderFunc func(context.Context, string) ([]byte, error)

func (f ProviderFunc) Resolve(ctx context.Context, ref string) ([]byte, error) {
	return f(ctx, ref)
}

func TestCachingProviderFreshWithinTTL(t *testing.T) {
	ctx := context.Background()
	ok := &atomic.Bool{}
	ok.Store(true)
	c, err := NewCachingProvider(controllableProvider(ok, []byte("material")), CacheConfig{TTL: time.Minute, StaleGrace: 30 * time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }

	value, err := c.Resolve(ctx, "secret://a")
	if err != nil || string(value) != "material" {
		t.Fatalf("first resolve = %q, %v", value, err)
	}
	// Within TTL the cache serves without calling the provider, even if it fails.
	ok.Store(false)
	now = now.Add(30 * time.Second)
	value, err = c.Resolve(ctx, "secret://a")
	if err != nil || string(value) != "material" {
		t.Fatalf("fresh cache resolve = %q, %v", value, err)
	}
	if c.Degraded("secret://a") {
		t.Fatal("fresh cache must not be degraded")
	}
}

func TestCachingProviderServesStaleWithinGrace(t *testing.T) {
	ctx := context.Background()
	ok := &atomic.Bool{}
	ok.Store(true)
	var outageRefs []string
	var outageDegraded []bool
	c, err := NewCachingProvider(controllableProvider(ok, []byte("material")), CacheConfig{TTL: time.Minute, StaleGrace: 30 * time.Second}, func(ref string, degraded bool) {
		outageRefs = append(outageRefs, ref)
		outageDegraded = append(outageDegraded, degraded)
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	if _, err := c.Resolve(ctx, "secret://a"); err != nil {
		t.Fatal(err)
	}
	// Past TTL but within TTL+grace with the provider down.
	ok.Store(false)
	now = now.Add(70 * time.Second)
	value, err := c.Resolve(ctx, "secret://a")
	if err != nil || string(value) != "material" {
		t.Fatalf("stale-within-grace resolve = %q, %v", value, err)
	}
	if !c.Degraded("secret://a") {
		t.Fatal("expected degraded flag")
	}
	if len(outageRefs) != 1 || outageRefs[0] != "secret://a" || !outageDegraded[0] {
		t.Fatalf("outage callback = %v %v", outageRefs, outageDegraded)
	}
}

func TestCachingProviderFailsClosedAfterGrace(t *testing.T) {
	ctx := context.Background()
	ok := &atomic.Bool{}
	ok.Store(true)
	c, err := NewCachingProvider(controllableProvider(ok, []byte("material")), CacheConfig{TTL: time.Minute, StaleGrace: 30 * time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	if _, err := c.Resolve(ctx, "secret://a"); err != nil {
		t.Fatal(err)
	}
	// Beyond TTL+grace with the provider down → fail closed.
	ok.Store(false)
	now = now.Add(2 * time.Minute)
	if _, err := c.Resolve(ctx, "secret://a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected fail-closed error, got %v", err)
	}
}

func TestCachingProviderRotationRefreshes(t *testing.T) {
	ctx := context.Background()
	ok := &atomic.Bool{}
	ok.Store(true)
	value := atomic.Value{}
	value.Store([]byte("old"))
	provider := ProviderFunc(func(context.Context, string) ([]byte, error) {
		if !ok.Load() {
			return nil, ErrNotFound
		}
		return value.Load().([]byte), nil
	})
	c, err := NewCachingProvider(provider, CacheConfig{TTL: time.Minute, StaleGrace: 30 * time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	if v, _ := c.Resolve(ctx, "secret://r"); string(v) != "old" {
		t.Fatalf("initial = %q", v)
	}
	// Rotation: the provider returns a new value after the cache TTL expires.
	value.Store([]byte("new"))
	now = now.Add(2 * time.Minute)
	ok.Store(true)
	if v, err := c.Resolve(ctx, "secret://r"); err != nil || string(v) != "new" {
		t.Fatalf("rotated resolve = %q, %v", v, err)
	}
}

func TestCrossTenantReferenceIsNonDisclosing(t *testing.T) {
	ctx := context.Background()
	secret := []byte("super-secret-material-value")
	provider := NewMemoryProvider(map[string][]byte{"secret://tenant-a/cred": secret})
	if _, err := provider.Resolve(ctx, "secret://tenant-b/cred"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant ref must be non-disclosing, got %v", err)
	}
	// Outage notification carries only the ref, never the material.
	c, _ := NewCachingProvider(provider, CacheConfig{}, func(ref string, _ bool) {
		if strings.Contains(ref, string(secret)) {
			t.Fatal("outage notification leaked material")
		}
	})
	if _, err := c.Resolve(ctx, "secret://tenant-b/cred"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cached cross-tenant resolve = %v", err)
	}
}

func TestEnvProvider(t *testing.T) {
	t.Setenv("LITEAIG_TEST_SECRET", "env-material")
	provider := NewEnvProvider()
	value, err := provider.Resolve(context.Background(), "secret://env/LITEAIG_TEST_SECRET")
	if err != nil || string(value) != "env-material" {
		t.Fatalf("env resolve = %q, %v", value, err)
	}
	if _, err := provider.Resolve(context.Background(), "secret://env/MISSING_VAR"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing env = %v", err)
	}
	if _, err := provider.Resolve(context.Background(), "secret://not-env/x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-env ref = %v", err)
	}
}

func TestValidateCacheConfig(t *testing.T) {
	if err := ValidateCacheConfig(CacheConfig{TTL: -time.Second}); err == nil {
		t.Fatal("negative TTL accepted")
	}
	if err := ValidateCacheConfig(CacheConfig{TTL: time.Minute, StaleGrace: 30 * time.Second}); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestLoadMasterKeyPrefersEnvOverSidecar(t *testing.T) {
	key, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LITEAIG_MASTER_KEY", base64.StdEncoding.EncodeToString(key))

	// With the env var set, a path whose sidecar does not exist must resolve
	// from the environment instead of generating a fresh sidecar file.
	got, err := LoadMasterKey(t.TempDir() + "/does-not-exist.db")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(key) {
		t.Fatal("LoadMasterKey did not honor LITEAIG_MASTER_KEY")
	}
}
