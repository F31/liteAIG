package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/controlplane/config"
	"github.com/F31/liteAIG/internal/controlplane/setup"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/platform/egress"
	"github.com/F31/liteAIG/internal/platform/secrets"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/internal/tenancy"
)

// providerProbe carries the wizard-session facts needed to re-probe a
// registered provider. The plaintext secret is never kept: Test and
// DiscoverModels resolve it from the encrypted vault.
type providerProbe struct {
	providerType string
	endpoint     string
	secretRef    string
}

// setupActorID is the well-formed UUID actor recorded on the bootstrapping
// config draft and publish audit rows. Postgres enforces the UUID column
// types (config_drafts.created_by/updated_by, audit_events.actor_id), so a
// free-text "setup" label is invalid there even though SQLite accepts it.
const setupActorID = "00000000-0000-4000-8000-000000000001"

// realProviderSetup is the Setup wizard provider backend: it validates the
// endpoint with a real health probe, stores the credential envelope-encrypted
// in the secret vault, and discovers models from the live provider catalog.
type realProviderSetup struct {
	ids      contracts.IDGenerator
	clock    contracts.Clock
	tenancy  tenancy.Repository
	config   *config.Service
	configDB config.Repository
	vault    *sqlrepo.SecretVault
	cipher   *secrets.Cipher
	http     *http.Client
	policy   egress.Policy

	mu     sync.Mutex
	probes map[string]providerProbe
}

func newRealProviderSetup(
	ids contracts.IDGenerator, clock contracts.Clock, tenancyRepo tenancy.Repository,
	configService *config.Service, configRepo config.Repository,
	vault *sqlrepo.SecretVault, cipher *secrets.Cipher, client *http.Client, policy egress.Policy,
) *realProviderSetup {
	if client == nil {
		client = egress.Client(egress.LitePolicy())
	}
	if policy.MaxResponseBytes == 0 {
		policy = egress.LitePolicy()
	}
	return &realProviderSetup{
		ids: ids, clock: clock, tenancy: tenancyRepo, config: configService, configDB: configRepo,
		vault: vault, cipher: cipher, http: client, policy: policy, probes: map[string]providerProbe{},
	}
}

// Register probes the endpoint for real, persists the secret, and records the
// probe facts for the wizard's Test / DiscoverModels steps.
func (p *realProviderSetup) Register(ctx context.Context, scope tenancy.TenantScope, _ string, input setup.ProviderInput) (setup.ProviderResult, error) {
	if input.Type != "openai" && input.Type != "openai-compatible" && input.Type != "anthropic" && input.Type != "azure-openai" && input.Type != "bedrock" && input.Type != "vertex" && input.Type != "gemini" {
		return setup.ProviderResult{}, fmt.Errorf("unsupported provider type %q", input.Type)
	}
	if input.Endpoint == "" || len(input.Secret) == 0 {
		return setup.ProviderResult{}, errors.New("provider endpoint and credential are required")
	}
	providerID, err := p.ids.New()
	if err != nil {
		return setup.ProviderResult{}, err
	}
	credentialID, err := p.ids.New()
	if err != nil {
		return setup.ProviderResult{}, err
	}
	secretRef := "local://credential/" + credentialID

	// Real reachability + auth probe before anything is recorded.
	if err := p.probe(ctx, input.Type, input.Endpoint, input.Secret); err != nil {
		return setup.ProviderResult{}, err
	}

	ciphertext, err := p.cipher.Encrypt(input.Secret)
	if err != nil {
		return setup.ProviderResult{}, fmt.Errorf("store provider credential: %w", err)
	}
	if err := p.vault.Put(ctx, secretRef, scope.TenantID, ciphertext); err != nil {
		return setup.ProviderResult{}, fmt.Errorf("store provider credential: %w", err)
	}

	p.mu.Lock()
	p.probes[providerID] = providerProbe{providerType: input.Type, endpoint: input.Endpoint, secretRef: secretRef}
	p.mu.Unlock()
	return setup.ProviderResult{ProviderID: providerID, SecretRef: secretRef}, nil
}

// Test re-probes the registered endpoint with the vault-resolved secret.
func (p *realProviderSetup) Test(ctx context.Context, scope tenancy.TenantScope, providerID string) error {
	probe, ok := p.lookup(scope, providerID)
	if !ok {
		return fmt.Errorf("unknown provider %s", providerID)
	}
	secret, err := p.resolveSecret(ctx, probe.secretRef)
	if err != nil {
		return err
	}
	defer clearBytes(secret)
	return p.probe(ctx, probe.providerType, probe.endpoint, secret)
}

// DiscoverModels lists models from the live provider catalog.
func (p *realProviderSetup) DiscoverModels(ctx context.Context, scope tenancy.TenantScope, providerID string) ([]string, error) {
	probe, ok := p.lookup(scope, providerID)
	if !ok {
		return nil, fmt.Errorf("unknown provider %s", providerID)
	}
	secret, err := p.resolveSecret(ctx, probe.secretRef)
	if err != nil {
		return nil, err
	}
	defer clearBytes(secret)
	return p.listModels(ctx, probe.providerType, probe.endpoint, secret)
}

func (p *realProviderSetup) lookup(scope tenancy.TenantScope, providerID string) (providerProbe, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	probe, ok := p.probes[providerID]
	return probe, ok
}

func (p *realProviderSetup) resolveSecret(ctx context.Context, ref string) ([]byte, error) {
	ciphertext, err := p.vault.Get(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("load provider credential: %w", err)
	}
	return p.cipher.Decrypt(ciphertext)
}

func clearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// probe performs a real HTTP check against the provider's model catalog. The
// endpoint is validated against the egress policy up front so unregistered
// internal targets fail before any dial. The result distinguishes the failure
// classes the wizard must explain to the operator: an unreachable endpoint, a
// rejected credential (401/403 is NOT a healthy provider — it is an auth
// failure), and a provider that answered with an error.
func (p *realProviderSetup) probe(ctx context.Context, providerType, endpoint string, secret []byte) error {
	if err := egress.ValidateTarget(endpoint, p.policy); err != nil {
		return fmt.Errorf("%w: %v", setup.ErrProviderConnection, err)
	}
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(healthCtx, http.MethodGet, providerCatalogURL(providerType, endpoint), nil)
	if err != nil {
		return fmt.Errorf("%w: %v", setup.ErrProviderConnection, err)
	}
	applyProbeHeaders(request, providerType, secret)
	response, err := p.http.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", setup.ErrProviderConnection, err)
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		return nil
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: status %d", setup.ErrProviderAuth, response.StatusCode)
	default:
		return fmt.Errorf("%w: status %d", setup.ErrProviderError, response.StatusCode)
	}
}

// listModels calls the provider's real model catalog endpoint.
func (p *realProviderSetup) listModels(ctx context.Context, providerType, endpoint string, secret []byte) ([]string, error) {
	if err := egress.ValidateTarget(endpoint, p.policy); err != nil {
		return nil, err
	}
	modelsCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(modelsCtx, http.MethodGet, providerCatalogURL(providerType, endpoint), nil)
	if err != nil {
		return nil, err
	}
	applyProbeHeaders(request, providerType, secret)
	response, err := p.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", setup.ErrProviderConnection, err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("%w: status %d", setup.ErrProviderAuth, response.StatusCode)
		}
		return nil, fmt.Errorf("%w: status %d", setup.ErrProviderError, response.StatusCode)
	}
	var catalog struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(response.Body).Decode(&catalog); err != nil {
		return nil, fmt.Errorf("%w: %v", setup.ErrProviderError, err)
	}
	models := make([]string, 0, len(catalog.Data))
	for _, item := range catalog.Data {
		if item.ID != "" {
			models = append(models, item.ID)
		}
	}
	for _, item := range catalog.Models {
		if item.Name != "" {
			models = append(models, strings.TrimPrefix(item.Name, "models/"))
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("%w: empty model catalog", setup.ErrProviderError)
	}
	return models, nil
}

// ConfigureDefault builds a complete tenant config (provider with real
// endpoint/type, encrypted credential, deployment, route policy, logical
// model) and publishes it so the runtime snapshot activates.
func (p *realProviderSetup) ConfigureDefault(ctx context.Context, scope tenancy.TenantScope, projectID, providerID, selectedModel string) error {
	tenant, err := p.tenancy.GetTenant(ctx, scope)
	if err != nil {
		return err
	}
	project, err := p.tenancy.GetProject(ctx, scope, projectID)
	if err != nil {
		return err
	}
	probe, ok := p.lookup(scope, providerID)
	if !ok {
		return fmt.Errorf("unknown provider %s", providerID)
	}
	deploymentID, err := p.ids.New()
	if err != nil {
		return err
	}
	routeID, err := p.ids.New()
	if err != nil {
		return err
	}
	modelID, err := p.ids.New()
	if err != nil {
		return err
	}
	now := p.clock.Now()

	providerType := probe.providerType
	if providerType == "openai-compatible" {
		providerType = "openai"
	}
	doc := config.TenantConfig{
		SchemaVersion: config.SchemaV1,
		Tenant: config.TenantResource{
			ID: tenant.ID, PublicRef: tenant.PublicRef, Status: tenant.Status,
		},
		Projects: []config.Project{{
			ID: project.ID, TenantID: project.TenantID, Status: project.Status,
			AllowedDataRegions: project.AllowedDataRegions, ResidencyEnforcement: project.ResidencyEnforcement,
		}},
		Providers: []config.Provider{{
			ID: providerID, TenantID: scope.TenantID, OwnerScope: "TENANT_PRIVATE", Type: providerType,
			Endpoint: probe.endpoint, Status: "enabled",
		}},
		Credentials: []config.Credential{{
			ID: credentialIDFromRef(probe.secretRef), ProviderID: providerID, TenantID: scope.TenantID, OwnerScope: "TENANT_PRIVATE",
			SecretRef: probe.secretRef, Status: "enabled",
		}},
		Deployments: []config.Deployment{{
			ID: deploymentID, TenantID: scope.TenantID, ProviderID: providerID, CredentialID: credentialIDFromRef(probe.secretRef),
			UpstreamModel: selectedModel, DataRegion: "global", Capabilities: []string{"chat"},
			ContextWindow: 8192, Priority: 1, Status: "enabled",
		}},
		RoutePolicies: []config.RoutePolicy{{
			ID: routeID, TenantID: scope.TenantID, ProjectID: projectID, Strategy: "priority",
			DeploymentIDs: []string{deploymentID}, Weights: map[string]int{deploymentID: 1}, Version: 1,
		}},
		LogicalModels: []config.LogicalModel{{
			ID: modelID, TenantID: scope.TenantID, Alias: "default-chat", RoutePolicyID: routeID,
		}},
	}
	draftID, err := p.ids.New()
	if err != nil {
		return err
	}
	if err := p.configDB.CreateDraft(ctx, scope, config.Draft{
		ID: draftID, TenantID: scope.TenantID, BaseVersion: 0, Revision: 1, Status: "editing",
		Config: doc, CreatedBy: setupActorID, UpdatedBy: setupActorID, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return err
	}
	if _, _, err := p.config.Publish(ctx, scope, draftID, 1, setupActorID); err != nil {
		return fmt.Errorf("publish default tenant config: %w", err)
	}
	return nil
}

func credentialIDFromRef(secretRef string) string {
	return strings.TrimPrefix(secretRef, "local://credential/")
}

// providerCatalogURL selects the provider's model-catalog probe path. It
// mirrors the runtime healthprobe: OpenAI-compatible, Anthropic, Bedrock, and
// Vertex share /v1/models; Azure OpenAI scopes the catalog under /openai/models
// with an api-version.
func providerCatalogURL(providerType, endpoint string) string {
	base := strings.TrimRight(endpoint, "/")
	if providerType == "azure-openai" {
		return base + "/openai/models?api-version=2024-06-01"
	}
	if providerType == "gemini" {
		return base + "/v1beta/models"
	}
	return base + "/v1/models"
}

// applyProbeHeaders sets the provider-specific credential header for the
// catalog probe. Anthropic uses x-api-key; Azure uses api-key; OpenAI-compatible,
// Bedrock, and Vertex use the Bearer form. Bedrock/Vertex probes are shallow
// reachability checks (the wizard's model discovery for those providers relies
// on the signature-wrapped data-plane call path).
func applyProbeHeaders(request *http.Request, providerType string, secret []byte) {
	switch providerType {
	case "anthropic":
		request.Header.Set("x-api-key", string(secret))
		request.Header.Set("anthropic-version", "2023-06-01")
	case "azure-openai":
		request.Header.Set("api-key", string(secret))
	case "gemini":
		request.Header.Set("x-goog-api-key", string(secret))
	default:
		request.Header.Set("Authorization", "Bearer "+string(secret))
	}
}
