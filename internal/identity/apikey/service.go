package apikey

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/tenancy"
)

type CreateInput struct {
	TenantRef, ProjectID, Name               string
	ApplicationID, AgentID, ServiceAccountID string
	ExpiresAt                                *time.Time
	ModelAllowlist, IPAllowlist              []string
}

type CreateResult struct {
	Key    string
	Record Record
}

type Service struct {
	repository Repository
	secrets    SecretProvider
	tokens     *Generator
	ids        contracts.IDGenerator
	clock      contracts.Clock
	cipher     KeyCipher
}

func NewService(repository Repository, secrets SecretProvider, tokens *Generator, ids contracts.IDGenerator, clock contracts.Clock, cipher KeyCipher) (*Service, error) {
	if repository == nil || secrets == nil || tokens == nil || ids == nil || clock == nil || cipher == nil {
		return nil, errors.New("API key dependencies are required")
	}
	return &Service{repository: repository, secrets: secrets, tokens: tokens, ids: ids, clock: clock, cipher: cipher}, nil
}

func (s *Service) Create(ctx context.Context, scope tenancy.TenantScope, input CreateInput) (*CreateResult, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if input.ProjectID == "" || input.Name == "" {
		return nil, errors.New("project and key name are required")
	}
	bindings := 0
	for _, value := range []string{input.ApplicationID, input.AgentID, input.ServiceAccountID} {
		if value != "" {
			bindings++
		}
	}
	if bindings > 1 {
		return nil, errors.New("API key can bind only one principal")
	}
	for _, entry := range input.IPAllowlist {
		if net.ParseIP(entry) == nil {
			if _, _, err := net.ParseCIDR(entry); err != nil {
				return nil, errors.New("invalid IP allowlist entry")
			}
		}
	}
	pepperVersion, err := s.repository.ActivePepper(ctx)
	if err != nil {
		return nil, err
	}
	pepper, err := s.secrets.Resolve(ctx, pepperVersion.Ref)
	if err != nil {
		return nil, fmt.Errorf("resolve key pepper: %w", err)
	}
	pepperCopy := append([]byte(nil), pepper...)
	defer clear(pepperCopy)
	token, err := s.tokens.NewKey(input.TenantRef)
	if err != nil {
		return nil, err
	}
	id, err := s.ids.New()
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	if input.ExpiresAt != nil && !input.ExpiresAt.After(now) {
		return nil, errors.New("key expiry must be in the future")
	}
	keyCiphertext, err := s.cipher.Encrypt([]byte(token.String()))
	if err != nil {
		return nil, fmt.Errorf("persist virtual key: %w", err)
	}
	record := Record{ID: id, PublicID: token.PublicID, TenantID: scope.TenantID, ProjectID: input.ProjectID, Name: input.Name, HMACDigest: Digest(pepperCopy, token), PepperVersion: pepperVersion.Version, Fingerprint: Fingerprint(token), ApplicationID: input.ApplicationID, AgentID: input.AgentID, ServiceAccountID: input.ServiceAccountID, Status: "active", ExpiresAt: input.ExpiresAt, ModelAllowlist: append([]string(nil), input.ModelAllowlist...), IPAllowlist: append([]string(nil), input.IPAllowlist...), CreatedAt: now, KeyCiphertext: keyCiphertext}
	if err := s.repository.Create(ctx, scope, record); err != nil {
		return nil, err
	}
	return &CreateResult{Key: token.String(), Record: record}, nil
}

func (s *Service) Revoke(ctx context.Context, scope tenancy.TenantScope, id string) error {
	if id == "" {
		return errors.New("key id is required")
	}
	return s.repository.Revoke(ctx, scope, id)
}

// List returns every API key for the tenant (all statuses) for the admin view.
func (s *Service) List(ctx context.Context, scope tenancy.TenantScope) ([]Record, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return s.repository.List(ctx, scope)
}

// Reveal decrypts and returns the full virtual key for an existing record.
// The key must have been created with a storable ciphertext; legacy rows
// without one cannot be recovered.
func (s *Service) Reveal(ctx context.Context, scope tenancy.TenantScope, id string) (string, error) {
	if id == "" {
		return "", errors.New("key id is required")
	}
	record, err := s.repository.Get(ctx, scope, id)
	if err != nil {
		return "", err
	}
	if len(record.KeyCiphertext) == 0 {
		return "", errors.New("key material was not stored for this key")
	}
	plaintext, err := s.cipher.Decrypt(record.KeyCiphertext)
	if err != nil {
		return "", fmt.Errorf("reveal virtual key: %w", err)
	}
	return string(plaintext), nil
}
