// Package scim defines the user/group provisioning contract so external IdP
// directory changes can be applied, with a deterministic mock for tests. A full
// SCIM REST server is out of scope.
package scim

import (
	"context"
	"errors"
	"sync"
)

// User is a provisioned directory user.
type User struct {
	ExternalID  string
	Email       string
	DisplayName string
	Active      bool
	Groups      []string
}

// Provisioner applies directory changes.
type Provisioner interface {
	UpsertUser(context.Context, User) error
	DeactivateUser(context.Context, string) error
}

var ErrNotFound = errors.New("provisioned user not found")

// MockProvisioner is a deterministic in-memory provisioner for tests.
type MockProvisioner struct {
	mu    sync.Mutex
	Users map[string]User
	Calls int
}

// NewMockProvisioner builds an empty mock.
func NewMockProvisioner() *MockProvisioner {
	return &MockProvisioner{Users: map[string]User{}}
}

// UpsertUser inserts or updates a user by external id.
func (m *MockProvisioner) UpsertUser(_ context.Context, user User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls++
	m.Users[user.ExternalID] = user
	return nil
}

// DeactivateUser marks a user inactive.
func (m *MockProvisioner) DeactivateUser(_ context.Context, externalID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls++
	user, ok := m.Users[externalID]
	if !ok {
		return ErrNotFound
	}
	user.Active = false
	m.Users[externalID] = user
	return nil
}
