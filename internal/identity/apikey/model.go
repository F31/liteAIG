package apikey

import (
	"context"
	"time"

	"github.com/F31/liteAIG/internal/tenancy"
)

type Record struct {
	ID, PublicID, TenantID, ProjectID, Name                       string
	HMACDigest                                                    []byte
	PepperVersion                                                 int
	Fingerprint, ApplicationID, AgentID, ServiceAccountID, Status string
	ExpiresAt                                                     *time.Time
	ModelAllowlist, IPAllowlist                                   []string
	CreatedAt                                                     time.Time
	KeyCiphertext                                                 []byte
}

type PepperVersion struct {
	Version     int
	Ref, Status string
}

// KeyCipher is the envelope cipher used to persist the virtual key so it can be
// revealed on demand. It is not required for gateway verification.
type KeyCipher interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(data []byte) ([]byte, error)
}

type Repository interface {
	ActivePepper(context.Context) (PepperVersion, error)
	ListRuntimePeppers(context.Context) ([]PepperVersion, error)
	Create(context.Context, tenancy.TenantScope, Record) error
	Revoke(context.Context, tenancy.TenantScope, string) error
	ListActive(context.Context, tenancy.TenantScope) ([]Record, error)
	List(context.Context, tenancy.TenantScope) ([]Record, error)
	Get(context.Context, tenancy.TenantScope, string) (Record, error)
}

type SecretProvider interface {
	Resolve(context.Context, string) ([]byte, error)
}
