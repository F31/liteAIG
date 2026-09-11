package adminapi

import (
	"context"
	"encoding/json"
	"github.com/F31/liteAIG/internal/controlplane/setup"
	"testing"
)

type wizardRunner struct{ input setup.WizardInput }

func (w *wizardRunner) Run(_ context.Context, input setup.WizardInput) (*setup.WizardResult, error) {
	w.input = input
	w.input.AdminPassword = append([]byte(nil), input.AdminPassword...)
	w.input.ProviderSecret = append([]byte(nil), input.ProviderSecret...)
	return &setup.WizardResult{VirtualKey: "key", SDKExample: "sdk", RequestID: "request", TenantID: "tenant", ProjectID: "project"}, nil
}
func TestWizardSetupAdapterMapsOneTimeResult(t *testing.T) {
	wizard := &wizardRunner{}
	adapter := WizardSetupAdapter{Wizard: wizard}
	raw, _ := json.Marshal(map[string]string{"Username": "admin", "AdminPassword": "password", "TenantName": "tenant", "ProviderName": "provider", "ProviderType": "openai", "ProviderEndpoint": "https://provider", "ProviderSecret": "secret", "SelectedModel": "model"})
	value, err := adapter.Setup(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	result := value
	if result.VirtualKey != "key" || string(wizard.input.ProviderSecret) != "secret" {
		t.Fatalf("result=%+v input=%+v", result, wizard.input)
	}
}
