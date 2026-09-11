package setup

import (
	"context"
	"errors"
	"fmt"
	"github.com/F31/liteAIG/internal/identity/apikey"
	"github.com/F31/liteAIG/internal/tenancy"
)

type Bootstrapper interface {
	Bootstrap(context.Context, Input) (*Result, error)
}

// HalfStateResetter clears a previous wizard run that committed the initial
// admin/tenant/project but terminated before publishing its first config
// version. Bootstrappers that implement it keep a failed setup retryable from
// the Console instead of locking the installation behind
// ErrAlreadyInitialized.
type HalfStateResetter interface {
	ResetHalfInitialized(context.Context) (bool, error)
}
type ProviderInput struct {
	Name, Type, Endpoint string
	Secret               []byte
}
type ProviderResult struct{ ProviderID, SecretRef string }
type ProviderSetup interface {
	Register(context.Context, tenancy.TenantScope, string, ProviderInput) (ProviderResult, error)
	Test(context.Context, tenancy.TenantScope, string) error
	DiscoverModels(context.Context, tenancy.TenantScope, string) ([]string, error)
	ConfigureDefault(context.Context, tenancy.TenantScope, string, string, string) error
}
type KeyCreator interface {
	Create(context.Context, tenancy.TenantScope, apikey.CreateInput) (*apikey.CreateResult, error)
}
type FirstCallRunner interface {
	RunFirstCall(context.Context, string, string, string) (requestID string, inputTokens, outputTokens int64, err error)
}
type WizardConfig struct{ GatewayBaseURL, DefaultLogicalModel, FirstKeyName string }
type WizardInput struct {
	Username                                                 string
	AdminPassword                                            []byte
	TenantName, ProviderName, ProviderType, ProviderEndpoint string
	ProviderSecret                                           []byte
	SelectedModel                                            string
}
type WizardResult struct {
	TenantID, ProjectID, ProviderID, LogicalModel, VirtualKey, RequestID, SDKExample string
	InputTokens, OutputTokens                                                        int64
	DiscoveredModels                                                                 []string
	// FirstCallWarning is set when the optional verification call after the
	// publish failed; the configuration itself is live and the key valid.
	FirstCallWarning string
}
type Wizard struct {
	config     WizardConfig
	bootstrap  Bootstrapper
	providers  ProviderSetup
	keys       KeyCreator
	playground FirstCallRunner
}

func NewWizard(config WizardConfig, bootstrap Bootstrapper, providers ProviderSetup, keys KeyCreator, playground FirstCallRunner) (*Wizard, error) {
	if config.GatewayBaseURL == "" || config.DefaultLogicalModel == "" || config.FirstKeyName == "" || bootstrap == nil || providers == nil || keys == nil || playground == nil {
		return nil, errors.New("wizard configuration and dependencies are required")
	}
	return &Wizard{config: config, bootstrap: bootstrap, providers: providers, keys: keys, playground: playground}, nil
}
func (w *Wizard) Run(ctx context.Context, input WizardInput) (*WizardResult, error) {
	adminPassword := append([]byte(nil), input.AdminPassword...)
	providerSecret := append([]byte(nil), input.ProviderSecret...)
	defer clear(adminPassword)
	defer clear(providerSecret)
	bootstrap, err := w.bootstrap.Bootstrap(ctx, Input{Username: input.Username, Password: adminPassword, TenantName: input.TenantName})
	if errors.Is(err, ErrAlreadyInitialized) {
		// A previous run committed the initial admin but died before the first
		// publish, so the install is half-initialized and the Console is dead
		// (no published version to draft from). If the bootstrapper can clear
		// that half-state safely, roll it back and bootstrap again so the
		// wizard stays retryable.
		if resetter, ok := w.bootstrap.(HalfStateResetter); ok {
			cleaned, resetErr := resetter.ResetHalfInitialized(ctx)
			if resetErr != nil {
				return nil, resetErr
			}
			if cleaned {
				bootstrap, err = w.bootstrap.Bootstrap(ctx, Input{Username: input.Username, Password: adminPassword, TenantName: input.TenantName})
			}
		}
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	scope := tenancy.TenantScope{TenantID: bootstrap.TenantID}
	provider, err := w.providers.Register(ctx, scope, bootstrap.ProjectID, ProviderInput{Name: input.ProviderName, Type: input.ProviderType, Endpoint: input.ProviderEndpoint, Secret: providerSecret})
	clear(providerSecret)
	if err != nil {
		return nil, err
	}
	if err := w.providers.Test(ctx, scope, provider.ProviderID); err != nil {
		return nil, err
	}
	models, err := w.providers.DiscoverModels(ctx, scope, provider.ProviderID)
	if err != nil {
		return nil, err
	}
	if !containsModel(models, input.SelectedModel) {
		return nil, ErrModelNotFound
	}
	// Create the key before publishing so it is compiled into the active
	// runtime snapshot (bearer-token auth resolves keys from the snapshot).
	key, err := w.keys.Create(ctx, scope, apikey.CreateInput{TenantRef: bootstrap.TenantRef, ProjectID: bootstrap.ProjectID, Name: w.config.FirstKeyName, ModelAllowlist: []string{w.config.DefaultLogicalModel}})
	if err != nil {
		return nil, err
	}
	if err := w.providers.ConfigureDefault(ctx, scope, bootstrap.ProjectID, provider.ProviderID, input.SelectedModel); err != nil {
		return nil, err
	}
	requestID, inputTokens, outputTokens, err := w.playground.RunFirstCall(ctx, bootstrap.TenantID, bootstrap.ProjectID, w.config.DefaultLogicalModel)
	warning := ""
	if err != nil {
		// The config, key, and provider are all live; the verification call is
		// advisory. Failing the wizard here would report an error for an
		// installation that actually initialized, so surface it as a warning
		// instead and let the operator inspect the request explorer.
		warning = err.Error()
	}
	example := fmt.Sprintf("base_url=%s/v1\napi_key=%s\nmodel=%s", w.config.GatewayBaseURL, key.Key, w.config.DefaultLogicalModel)
	return &WizardResult{TenantID: bootstrap.TenantID, ProjectID: bootstrap.ProjectID, ProviderID: provider.ProviderID, LogicalModel: w.config.DefaultLogicalModel, VirtualKey: key.Key, RequestID: requestID, SDKExample: example, InputTokens: inputTokens, OutputTokens: outputTokens, DiscoveredModels: models, FirstCallWarning: warning}, nil
}
func containsModel(models []string, want string) bool {
	for _, model := range models {
		if model == want {
			return true
		}
	}
	return false
}
