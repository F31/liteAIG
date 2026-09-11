package setup

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRepository struct{ state InitialState }

func (r *fakeRepository) CreateInitial(_ context.Context, state InitialState) error {
	r.state = state
	return nil
}

type fakeHasher struct{ got []byte }

func (h *fakeHasher) Hash(password []byte) (string, error) {
	h.got = append([]byte(nil), password...)
	return "argon2id-hash", nil
}

type sequenceIDs struct{ next int }

func (g *sequenceIDs) New() (string, error) {
	g.next++
	return []string{"admin-id", "tenant-id", "project-id"}[g.next-1], nil
}

func (g *sequenceIDs) NewTenantRef() (string, error) { return "tenant-ref", nil }

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func TestBootstrapBuildsAtomicInitialState(t *testing.T) {
	repository := &fakeRepository{}
	hasher := &fakeHasher{}
	service, err := NewService(
		Config{DefaultProjectName: "Starter", SettlementCurrency: "CNY", MinimumPasswordSize: 8},
		repository,
		hasher,
		&sequenceIDs{},
		&sequenceIDs{},
		fixedClock{now: time.Unix(10, 0)},
	)
	if err != nil {
		t.Fatal(err)
	}
	password := []byte("password-value")
	result, err := service.Bootstrap(context.Background(), Input{
		Username:   "admin",
		Password:   password,
		TenantName: "Example",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProjectID != "project-id" || repository.state.Tenant.DefaultProjectID != "project-id" {
		t.Fatalf("Bootstrap() result = %+v, state = %+v", result, repository.state)
	}
	if repository.state.Project.Name != "Starter" || repository.state.Tenant.SettlementCurrency != "CNY" {
		t.Fatalf("Bootstrap() ignored configuration: %+v", repository.state)
	}
	if repository.state.Admin.PasswordHash != "argon2id-hash" || string(hasher.got) != string(password) {
		t.Fatal("Bootstrap() did not pass the password through the hasher")
	}
	if string(password) != "password-value" {
		t.Fatal("Bootstrap() mutated caller-owned password memory")
	}
}

func TestBootstrapRejectsShortPassword(t *testing.T) {
	service, err := NewService(DefaultConfig(), &fakeRepository{}, &fakeHasher{}, &sequenceIDs{}, &sequenceIDs{}, fixedClock{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Bootstrap(context.Background(), Input{
		Username:   "admin",
		Password:   []byte("short"),
		TenantName: "Example",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Bootstrap() error = %v", err)
	}
}
