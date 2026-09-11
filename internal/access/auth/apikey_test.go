package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/identity/apikey"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

type secretMap map[string][]byte

func (s secretMap) Resolve(_ context.Context, ref string) ([]byte, error) {
	value, ok := s[ref]
	if !ok {
		return nil, errors.New("missing")
	}
	return value, nil
}

type testClock struct{ now time.Time }

func (c testClock) Now() time.Time { return c.now }

func TestSnapshotOnlyAPIKeyAuthentication(t *testing.T) {
	now := time.Unix(100, 0)
	tenantRef := "11111111111111111111111111111111"
	token := apikey.Token{TenantRef: tenantRef, PublicID: "22222222222222222222222222222222", Secret: "3333333333333333333333333333333333333333333333333333333333333333"}
	pepper := []byte("pepper")
	key := runtime.APIKey{ID: "key-1", PublicID: token.PublicID, ProjectID: "project-1", ApplicationID: "app-1", Status: "active", HMACDigest: apikey.Digest(pepper, token), PepperVersion: 1, IPAllowlist: []string{"10.0.0.0/8"}}
	registry := &runtime.ActiveRegistry{}
	registry.ActivateGlobal(runtime.NewGlobalRuntime(runtime.GlobalRuntimeData{PepperRefs: map[int]string{1: "secret://pepper/1"}}))
	registry.ActivateTenant(tenantRef, runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "tenant-1", TenantRef: tenantRef, Status: "active", APIKeys: []runtime.APIKey{key}}))
	authenticator := NewAPIKeyAuthenticator(registry, secretMap{"secret://pepper/1": pepper}, testClock{now})
	result, err := authenticator.Authenticate(context.Background(), token.String(), RequestScope{TenantID: "tenant-1", ProjectID: "project-1", RemoteIP: "10.1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Principal.Type != "application" || result.Principal.ProjectID != "project-1" {
		t.Fatalf("principal=%+v", result.Principal)
	}

	wrong := token
	wrong.Secret = "4444444444444444444444444444444444444444444444444444444444444444"
	unknown := token
	unknown.PublicID = "55555555555555555555555555555555"
	cases := map[string]struct {
		raw   string
		scope RequestScope
	}{"malformed": {"bad", RequestScope{}}, "wrong_secret": {wrong.String(), RequestScope{}}, "unknown_key": {unknown.String(), RequestScope{}}, "tenant_override": {token.String(), RequestScope{TenantID: "other"}}, "project_override": {token.String(), RequestScope{ProjectID: "other"}}, "ip_denied": {token.String(), RequestScope{RemoteIP: "192.168.1.1"}}}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := authenticator.Authenticate(context.Background(), tt.raw, tt.scope)
			if err == nil || !errors.Is(err, ErrUnauthorized) || err.Error() != ErrUnauthorized.Error() {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestAPIKeyAuthenticationRejectsRevokedExpiredSuspendedAndCrossTenant(t *testing.T) {
	now := time.Unix(100, 0)
	refA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	refB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	publicID := "cccccccccccccccccccccccccccccccc"
	secret := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	pepper := []byte("pepper")
	token := apikey.Token{TenantRef: refA, PublicID: publicID, Secret: secret}
	past := time.Unix(99, 0)
	registry := &runtime.ActiveRegistry{}
	registry.ActivateGlobal(runtime.NewGlobalRuntime(runtime.GlobalRuntimeData{PepperRefs: map[int]string{1: "pepper-ref"}}))
	activeA := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "tenant-a", TenantRef: refA, Status: "active", APIKeys: []runtime.APIKey{{PublicID: publicID, Status: "active", HMACDigest: apikey.Digest(pepper, token), PepperVersion: 1, ExpiresAt: &past}}})
	registry.ActivateTenant(refA, activeA)
	registry.ActivateTenant(refB, runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "tenant-b", TenantRef: refB, Status: "active", Providers: []runtime.Provider{{ID: "provider-b"}}, Deployments: []runtime.Deployment{{ID: "deployment-b"}}, LogicalModels: []runtime.LogicalModel{{ID: "logical-b", Alias: "model-b"}}, APIKeys: []runtime.APIKey{{PublicID: publicID, Status: "active", HMACDigest: []byte("other"), PepperVersion: 1}}}))
	if _, ok := activeA.Provider("provider-b"); ok {
		t.Fatal("tenant A exposed tenant B provider")
	}
	if _, ok := activeA.Deployment("deployment-b"); ok {
		t.Fatal("tenant A exposed tenant B deployment")
	}
	if _, ok := activeA.LogicalModel("model-b"); ok {
		t.Fatal("tenant A exposed tenant B logical model")
	}
	authenticator := NewAPIKeyAuthenticator(registry, secretMap{"pepper-ref": pepper}, testClock{now})
	if _, err := authenticator.Authenticate(context.Background(), token.String(), RequestScope{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired error=%v", err)
	}
	revoked := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "tenant-a", TenantRef: refA, Status: "active", APIKeys: []runtime.APIKey{{PublicID: publicID, Status: "revoked", HMACDigest: apikey.Digest(pepper, token), PepperVersion: 1}}})
	registry.ActivateTenant(refA, revoked)
	if _, err := authenticator.Authenticate(context.Background(), token.String(), RequestScope{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked error=%v", err)
	}
	suspended := runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: "tenant-a", TenantRef: refA, Status: "suspended"})
	registry.ActivateTenant(refA, suspended)
	if _, err := authenticator.Authenticate(context.Background(), token.String(), RequestScope{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("suspended error=%v", err)
	}
	if _, ok := suspended.Provider("provider-b"); ok {
		t.Fatal("tenant A snapshot exposed tenant B provider")
	}
}
