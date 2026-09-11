package setup

import (
	"context"
	"errors"
	"github.com/F31/liteAIG/internal/identity/apikey"
	"github.com/F31/liteAIG/internal/tenancy"
	"testing"
)

type bootstrapper struct{}

func (bootstrapper) Bootstrap(context.Context, Input) (*Result, error) {
	return &Result{TenantID: "tenant", TenantRef: "11111111111111111111111111111111", ProjectID: "project"}, nil
}

type providers struct {
	secret             []byte
	tested, configured bool
}

func (p *providers) Register(_ context.Context, _ tenancy.TenantScope, _ string, input ProviderInput) (ProviderResult, error) {
	p.secret = append([]byte(nil), input.Secret...)
	return ProviderResult{ProviderID: "provider", SecretRef: "secret://provider"}, nil
}
func (p *providers) Test(context.Context, tenancy.TenantScope, string) error {
	p.tested = true
	return nil
}
func (p *providers) DiscoverModels(context.Context, tenancy.TenantScope, string) ([]string, error) {
	return []string{"physical-model"}, nil
}
func (p *providers) ConfigureDefault(context.Context, tenancy.TenantScope, string, string, string) error {
	p.configured = true
	return nil
}

type keys struct{}

func (keys) Create(context.Context, tenancy.TenantScope, apikey.CreateInput) (*apikey.CreateResult, error) {
	return &apikey.CreateResult{Key: "one-time-key"}, nil
}

type firstCall struct{ err error }

func (f firstCall) RunFirstCall(context.Context, string, string, string) (string, int64, int64, error) {
	if f.err != nil {
		return "", 0, 0, f.err
	}
	return "request-1", 2, 1, nil
}
func TestWizardCompletesFirstCallAndReturnsOneTimeArtifacts(t *testing.T) {
	providers := &providers{}
	wizard, err := NewWizard(WizardConfig{GatewayBaseURL: "https://gateway.example", DefaultLogicalModel: "default-chat", FirstKeyName: "first-key"}, bootstrapper{}, providers, keys{}, firstCall{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := wizard.Run(context.Background(), WizardInput{Username: "admin", AdminPassword: []byte("password"), TenantName: "Tenant", ProviderName: "Provider", ProviderType: "openai", ProviderSecret: []byte("provider-secret"), SelectedModel: "physical-model"})
	if err != nil {
		t.Fatal(err)
	}
	if !providers.tested || !providers.configured || string(providers.secret) != "provider-secret" || result.VirtualKey != "one-time-key" || result.RequestID != "request-1" || result.SDKExample == "" {
		t.Fatalf("providers=%+v result=%+v", providers, result)
	}
}

func TestWizardSurfacesWarningWhenFirstCallFails(t *testing.T) {
	wizard, err := NewWizard(WizardConfig{GatewayBaseURL: "https://gateway.example", DefaultLogicalModel: "default-chat", FirstKeyName: "first-key"}, bootstrapper{}, &providers{}, keys{}, firstCall{err: errors.New("provider rate limited")})
	if err != nil {
		t.Fatal(err)
	}
	result, err := wizard.Run(context.Background(), WizardInput{Username: "admin", AdminPassword: []byte("password"), TenantName: "Tenant", ProviderName: "Provider", ProviderType: "openai", ProviderSecret: []byte("provider-secret"), SelectedModel: "physical-model"})
	if err != nil {
		t.Fatalf("a failed verification call must not fail the wizard: %v", err)
	}
	if result.VirtualKey != "one-time-key" || result.FirstCallWarning == "" {
		t.Fatalf("result=%+v want key plus a first-call warning", result)
	}
}

// resumableBootstrapper is a fake Bootstrapper that also implements
// HalfStateResetter: it reports ErrAlreadyInitialized once (a previous
// half-run) and boots cleanly after a reset.
type resumableBootstrapper struct {
	failures       int
	cleaned        bool
	resetRefused   bool
	resetFailures  int
	bootstrapCalls int
}

func (r *resumableBootstrapper) Bootstrap(context.Context, Input) (*Result, error) {
	r.bootstrapCalls++
	if r.bootstrapCalls <= r.failures {
		return nil, ErrAlreadyInitialized
	}
	return &Result{TenantID: "tenant", TenantRef: "22222222222222222222222222222222", ProjectID: "project"}, nil
}

func (r *resumableBootstrapper) ResetHalfInitialized(context.Context) (bool, error) {
	r.cleaned = true
	r.resetFailures++
	if r.resetRefused {
		return false, nil
	}
	return true, nil
}

func TestWizardRetriesAfterClearingHalfInitializedState(t *testing.T) {
	resetter := &resumableBootstrapper{failures: 1}
	wizard, err := NewWizard(WizardConfig{GatewayBaseURL: "https://gateway.example", DefaultLogicalModel: "default-chat", FirstKeyName: "first-key"}, resetter, &providers{}, keys{}, firstCall{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := wizard.Run(context.Background(), WizardInput{Username: "admin", AdminPassword: []byte("password"), TenantName: "Tenant", ProviderName: "Provider", ProviderType: "openai", ProviderSecret: []byte("provider-secret"), SelectedModel: "physical-model"})
	if err != nil {
		t.Fatal(err)
	}
	if result.VirtualKey != "one-time-key" {
		t.Fatalf("result=%+v", result)
	}
	if !resetter.cleaned || resetter.bootstrapCalls != 2 {
		t.Fatalf("expected one reset and a second bootstrap attempt, got %+v", resetter)
	}
}

func TestWizardReportsAlreadyInitializedWhenResetRefused(t *testing.T) {
	resetter := &resumableBootstrapper{failures: 1, resetRefused: true}
	wizard, err := NewWizard(WizardConfig{GatewayBaseURL: "https://gateway.example", DefaultLogicalModel: "default-chat", FirstKeyName: "first-key"}, resetter, &providers{}, keys{}, firstCall{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = wizard.Run(context.Background(), WizardInput{Username: "admin", AdminPassword: []byte("password"), TenantName: "Tenant", ProviderName: "Provider", ProviderType: "openai", ProviderSecret: []byte("provider-secret"), SelectedModel: "physical-model"})
	if !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("err = %v, want ErrAlreadyInitialized", err)
	}
	if resetter.bootstrapCalls != 1 {
		t.Fatalf("bootstrapper calls = %d, want 1 (reset refused, no retry)", resetter.bootstrapCalls)
	}
}
