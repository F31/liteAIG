// Package setup implements the one-time Lite installation bootstrap.
package setup

import (
	"context"
	"errors"
	"fmt"

	"github.com/F31/liteAIG/internal/controlplane/rbac"
	"github.com/F31/liteAIG/internal/identity"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/tenancy"
)

var (
	ErrAlreadyInitialized = errors.New("LiteAIG is already initialized")
	ErrInvalidInput       = errors.New("invalid bootstrap input")
	ErrProviderConnection = errors.New("provider connection failed")
	ErrProviderAuth       = errors.New("provider credential rejected")
	ErrProviderError      = errors.New("provider endpoint error")
	ErrModelNotFound      = errors.New("selected model not discovered")
)

type PasswordHasher interface {
	Hash([]byte) (string, error)
}

type Repository interface {
	CreateInitial(context.Context, InitialState) error
}

type TenantRefGenerator interface{ NewTenantRef() (string, error) }

type Config struct {
	DefaultProjectName  string
	SettlementCurrency  string
	MinimumPasswordSize int
}

func DefaultConfig() Config {
	return Config{
		DefaultProjectName:  "Default Project",
		SettlementCurrency:  "USD",
		MinimumPasswordSize: 8,
	}
}

func (c Config) validate() error {
	if c.DefaultProjectName == "" || c.SettlementCurrency == "" || c.MinimumPasswordSize <= 0 {
		return errors.New("invalid setup configuration")
	}
	return nil
}

type Input struct {
	Username   string
	Password   []byte
	TenantName string
}

type InitialState struct {
	Admin   identity.LocalAdmin
	Tenant  tenancy.Tenant
	Project tenancy.Project
}

type Result struct {
	AdminID   string
	TenantID  string
	TenantRef string
	ProjectID string
}

type Service struct {
	config     Config
	repo       Repository
	hasher     PasswordHasher
	ids        contracts.IDGenerator
	tenantRefs TenantRefGenerator
	clock      contracts.Clock
}

func NewService(
	config Config,
	repo Repository,
	hasher PasswordHasher,
	ids contracts.IDGenerator,
	tenantRefs TenantRefGenerator,
	clock contracts.Clock,
) (*Service, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	if repo == nil || hasher == nil || ids == nil || tenantRefs == nil || clock == nil {
		return nil, errors.New("setup dependencies are required")
	}
	return &Service{config: config, repo: repo, hasher: hasher, ids: ids, tenantRefs: tenantRefs, clock: clock}, nil
}

func (s *Service) Bootstrap(ctx context.Context, input Input) (*Result, error) {
	if input.Username == "" || input.TenantName == "" || len(input.Password) < s.config.MinimumPasswordSize {
		return nil, ErrInvalidInput
	}
	password := append([]byte(nil), input.Password...)
	defer clear(password)
	passwordHash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, fmt.Errorf("hash local admin password: %w", err)
	}

	values := make([]string, 3)
	for i := range values {
		values[i], err = s.ids.New()
		if err != nil {
			return nil, err
		}
	}
	tenantRef, err := s.tenantRefs.NewTenantRef()
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	state := InitialState{
		Admin: identity.LocalAdmin{
			ID:           values[0],
			Username:     input.Username,
			PasswordHash: passwordHash,
			Role:         rbac.RoleTenantAdmin,
			Status:       "active",
			CreatedAt:    now,
		},
		Tenant: tenancy.Tenant{
			ID:                 values[1],
			PublicRef:          tenantRef,
			Name:               input.TenantName,
			Status:             "active",
			SettlementCurrency: s.config.SettlementCurrency,
			DefaultProjectID:   values[2],
			CreatedAt:          now,
		},
		Project: tenancy.Project{
			ID:                   values[2],
			TenantID:             values[1],
			Name:                 s.config.DefaultProjectName,
			Status:               "active",
			ResidencyEnforcement: "advisory",
			CreatedAt:            now,
		},
	}
	if err := s.repo.CreateInitial(ctx, state); err != nil {
		return nil, err
	}
	return &Result{AdminID: values[0], TenantID: values[1], TenantRef: tenantRef, ProjectID: values[2]}, nil
}
