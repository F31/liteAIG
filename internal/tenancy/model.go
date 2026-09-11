// Package tenancy owns Tenant and Project resource semantics.
package tenancy

import (
	"errors"
	"time"
)

var (
	ErrInvalidScope  = errors.New("invalid tenant scope")
	ErrScopeMismatch = errors.New("resource is outside tenant scope")
	ErrNotFound      = errors.New("tenant resource not found")
)

// TenantScope is mandatory for every repository operation on existing tenant data.
type TenantScope struct {
	TenantID string
}

func (s TenantScope) Validate() error {
	if s.TenantID == "" {
		return ErrInvalidScope
	}
	return nil
}

type Tenant struct {
	ID                 string
	PublicRef          string
	Name               string
	Status             string
	SettlementCurrency string
	DefaultProjectID   string
	CreatedAt          time.Time
}

type Project struct {
	ID                   string
	TenantID             string
	Name                 string
	Status               string
	ResidencyEnforcement string
	AllowedDataRegions   []string
	CreatedAt            time.Time
}

type PageRequest struct {
	Offset int
	Limit  int
}

func (p PageRequest) Validate() error {
	if p.Offset < 0 || p.Limit <= 0 {
		return errors.New("invalid page request")
	}
	return nil
}

type ProjectPage struct {
	Items      []Project
	NextOffset *int
}

type TenantPage struct {
	Items      []Tenant
	NextOffset *int
}
