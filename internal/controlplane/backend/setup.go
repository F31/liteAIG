package backend

import (
	"context"
	"encoding/json"
	"github.com/F31/liteAIG/internal/controlplane/setup"
)

type WizardRunner interface {
	Run(context.Context, setup.WizardInput) (*setup.WizardResult, error)
}
type WizardSetupAdapter struct{ Wizard WizardRunner }
type WizardSetupResponse struct {
	VirtualKey string `json:"virtualKey"`
	SDKExample string `json:"sdkExample"`
	RequestID  string `json:"requestId"`
	TenantID   string `json:"tenantId"`
	ProjectID  string `json:"projectId"`
	// FirstCallWarning carries a failure from the post-publish verification
	// call; the installation itself is live.
	FirstCallWarning string `json:"firstCallWarning,omitempty"`
}

func (a WizardSetupAdapter) Setup(ctx context.Context, raw json.RawMessage) (WizardSetupResponse, error) {
	var input struct{ Username, AdminPassword, TenantName, ProviderName, ProviderType, ProviderEndpoint, ProviderSecret, SelectedModel string }
	if err := json.Unmarshal(raw, &input); err != nil {
		return WizardSetupResponse{}, err
	}
	adminPassword := []byte(input.AdminPassword)
	providerSecret := []byte(input.ProviderSecret)
	input.AdminPassword, input.ProviderSecret = "", ""
	defer clear(adminPassword)
	defer clear(providerSecret)
	result, err := a.Wizard.Run(ctx, setup.WizardInput{Username: input.Username, AdminPassword: adminPassword, TenantName: input.TenantName, ProviderName: input.ProviderName, ProviderType: input.ProviderType, ProviderEndpoint: input.ProviderEndpoint, ProviderSecret: providerSecret, SelectedModel: input.SelectedModel})
	if err != nil {
		return WizardSetupResponse{}, err
	}
	return WizardSetupResponse{VirtualKey: result.VirtualKey, SDKExample: result.SDKExample, RequestID: result.RequestID, TenantID: result.TenantID, ProjectID: result.ProjectID, FirstCallWarning: result.FirstCallWarning}, nil
}
