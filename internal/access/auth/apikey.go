// Package auth resolves canonical principals from immutable Runtime views.
package auth

import (
	"context"
	"errors"
	"net"

	"github.com/F31/liteAIG/internal/identity"
	"github.com/F31/liteAIG/internal/identity/apikey"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

var ErrUnauthorized = errors.New("invalid credentials")

type RequestScope struct{ TenantID, ProjectID, RemoteIP string }
type Result struct {
	Principal identity.Principal
	Snapshot  *runtime.TenantRuntimeSnapshot
	Key       runtime.APIKey
}

type APIKeyAuthenticator struct {
	registry runtime.Registry
	secrets  apikey.SecretProvider
	clock    contracts.Clock
}

func NewAPIKeyAuthenticator(registry runtime.Registry, secrets apikey.SecretProvider, clock contracts.Clock) *APIKeyAuthenticator {
	return &APIKeyAuthenticator{registry: registry, secrets: secrets, clock: clock}
}

func (a *APIKeyAuthenticator) Authenticate(ctx context.Context, raw string, requested RequestScope) (*Result, error) {
	token, err := apikey.Parse(raw)
	if err != nil {
		return nil, ErrUnauthorized
	}
	snapshot, ok := a.registry.Tenant(token.TenantRef)
	if !ok || snapshot.Status != "active" {
		return nil, ErrUnauthorized
	}
	key, ok := snapshot.APIKey(token.PublicID)
	if !ok {
		apikey.Digest(make([]byte, 32), token)
		return nil, ErrUnauthorized
	}
	global := a.registry.Global()
	if global == nil {
		return nil, ErrUnauthorized
	}
	pepperRef, ok := global.PepperRef(key.PepperVersion)
	if !ok {
		return nil, ErrUnauthorized
	}
	pepper, err := a.secrets.Resolve(ctx, pepperRef)
	if err != nil {
		return nil, ErrUnauthorized
	}
	pepperCopy := append([]byte(nil), pepper...)
	defer clear(pepperCopy)
	if !apikey.Verify(pepperCopy, token, key.HMACDigest) || key.Status != "active" {
		return nil, ErrUnauthorized
	}
	if key.ExpiresAt != nil && !key.ExpiresAt.After(a.clock.Now()) {
		return nil, ErrUnauthorized
	}
	if requested.TenantID != "" && requested.TenantID != snapshot.TenantID {
		return nil, ErrUnauthorized
	}
	if requested.ProjectID != "" && requested.ProjectID != key.ProjectID {
		return nil, ErrUnauthorized
	}
	if !allowedIP(requested.RemoteIP, key.IPAllowlist) {
		return nil, ErrUnauthorized
	}
	principal := identity.Principal{Type: "api_key", TenantID: snapshot.TenantID, ProjectID: key.ProjectID, APIKeyID: key.ID, ApplicationID: key.ApplicationID, AgentID: key.AgentID, ServiceAccountID: key.ServiceAccountID, AuthMethod: "api_key", AttributionTrust: "key_bound"}
	switch {
	case key.AgentID != "":
		principal.Type = "agent"
	case key.ApplicationID != "":
		principal.Type = "application"
	case key.ServiceAccountID != "":
		principal.Type = "service_account"
	}
	return &Result{Principal: principal, Snapshot: snapshot, Key: key}, nil
}

func allowedIP(value string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return true
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return false
	}
	for _, entry := range allowlist {
		if candidate := net.ParseIP(entry); candidate != nil && candidate.Equal(ip) {
			return true
		}
		if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}
