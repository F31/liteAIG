package golden

import (
	"context"
	"fmt"
	"github.com/F31/liteAIG/internal/access/auth"
	"github.com/F31/liteAIG/internal/connectors/model/anthropic"
	openconnector "github.com/F31/liteAIG/internal/connectors/model/openai"
	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/gateway/admission"
	"github.com/F31/liteAIG/internal/gateway/execution"
	gatewaymodel "github.com/F31/liteAIG/internal/gateway/model"
	"github.com/F31/liteAIG/internal/identity/apikey"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/internal/resilience/retry"
	routing "github.com/F31/liteAIG/internal/routing/model"
	"github.com/F31/liteAIG/internal/tenancy"
	"github.com/F31/liteAIG/migrations"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type secretStore map[string][]byte

func (s secretStore) Resolve(_ context.Context, ref string) ([]byte, error) { return s[ref], nil }

type clock struct{}

func (clock) Now() time.Time { return time.Now().UTC() }

type ids struct{ next int }

func (i *ids) New() (string, error) {
	i.next++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", i.next), nil
}
func TestScenarioAMultiModelUnifiedEgress(t *testing.T) {
	started := time.Now()
	openServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"open","model":"open-physical","created":1,"choices":[{"index":0,"message":{"role":"assistant","content":"openai"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer openServer.Close()
	anthropicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"anthropic","model":"claude-physical","content":[{"type":"text","text":"anthropic"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":1}}`))
	}))
	defer anthropicServer.Close()
	secrets := secretStore{"secret://open": []byte("open-secret"), "secret://anthropic": []byte("anthropic-secret"), "secret://pepper": []byte("pepper")}
	openConfig := openconnector.DefaultConfig()
	openConfig.BaseURL = openServer.URL
	openConfig.SecretRef = "secret://open"
	openConfig.AllowInsecureHTTP = true
	openInvoker, err := openconnector.NewCompatible(openConfig, openServer.Client(), secrets)
	if err != nil {
		t.Fatal(err)
	}
	anthropicConfig := anthropic.DefaultConfig()
	anthropicConfig.BaseURL = anthropicServer.URL
	anthropicConfig.SecretRef = "secret://anthropic"
	anthropicConfig.AllowInsecureHTTP = true
	anthropicInvoker, err := anthropic.New(anthropicConfig, anthropicServer.Client(), secrets)
	if err != nil {
		t.Fatal(err)
	}
	policy := retry.DefaultPolicy()
	policy.BaseBackoff = time.Nanosecond
	executor := execution.New(policy, execution.StaticResolver{"open": openInvoker, "anthropic": anthropicInvoker}, nil, retry.TimerSleeper{}, func() time.Duration { return 0 })
	modelService := gatewaymodel.New(&routing.Planner{}, executor)
	tenantID := "10000000-0000-4000-8000-000000000001"
	projectID := "10000000-0000-4000-8000-000000000002"
	tenantRef := "11111111111111111111111111111111"
	tokens := make([]apikey.Token, 10)
	runtimeKeys := make([]runtime.APIKey, 10)
	for i := range tokens {
		publicID := fmt.Sprintf("%032x", i+1)
		secret := fmt.Sprintf("%064x", i+1)
		tokens[i] = apikey.Token{TenantRef: tenantRef, PublicID: publicID, Secret: secret}
		runtimeKeys[i] = runtime.APIKey{ID: fmt.Sprintf("key-%d", i), PublicID: publicID, ProjectID: projectID, ApplicationID: fmt.Sprintf("app-%d", i), Status: "active", HMACDigest: apikey.Digest([]byte("pepper"), tokens[i]), PepperVersion: 1, ModelAllowlist: []string{"default-chat"}}
	}
	snapshotFor := func(openPriority, anthropicPriority int) *runtime.TenantRuntimeSnapshot {
		return runtime.NewTenantSnapshot(runtime.TenantSnapshotData{TenantID: tenantID, TenantRef: tenantRef, Status: "active", Version: int64(openPriority + 2), Projects: []runtime.Project{{ID: projectID, Status: "active"}}, Providers: []runtime.Provider{{ID: "open-provider", Status: "enabled"}, {ID: "anthropic-provider", Status: "enabled"}}, Credentials: []runtime.Credential{{ID: "open-credential", Status: "enabled"}, {ID: "anthropic-credential", Status: "enabled"}}, Deployments: []runtime.Deployment{{ID: "open", ProviderID: "open-provider", CredentialID: "open-credential", Status: "enabled", Priority: openPriority, Capabilities: []string{"chat"}}, {ID: "anthropic", ProviderID: "anthropic-provider", CredentialID: "anthropic-credential", Status: "enabled", Priority: anthropicPriority, Capabilities: []string{"chat"}}}, RoutePolicies: []runtime.RoutePolicy{{ID: "route", ProjectID: projectID, Strategy: "priority", DeploymentIDs: []string{"open", "anthropic"}, Version: 1}}, LogicalModels: []runtime.LogicalModel{{ID: "logical", Alias: "default-chat", RoutePolicyID: "route"}}, APIKeys: runtimeKeys})
	}
	registry := &runtime.ActiveRegistry{}
	registry.ActivateGlobal(runtime.NewGlobalRuntime(runtime.GlobalRuntimeData{PepperRefs: map[int]string{1: "secret://pepper"}}))
	registry.ActivateTenant(tenantRef, snapshotFor(0, 1))
	authenticator := auth.NewAPIKeyAuthenticator(registry, secrets, clock{})
	idSource := &ids{}
	admissionService, _ := admission.New(admission.Config{MaxBodyBytes: 1 << 20}, authenticator, idSource, clock{})
	db, err := sqlite.Open(context.Background(), "file:golden-a?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tenants(id,public_ref,name) VALUES ($1,$2,'Golden')`, tenantID, tenantRef); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id,tenant_id,name) VALUES ($1,$2,'Default')`, projectID, tenantID); err != nil {
		t.Fatal(err)
	}
	accountingRepo := sqlrepo.NewAccountingRepository(db)
	selected := map[string]int{}
	for i, token := range tokens {
		if i == 5 {
			registry.ActivateTenant(tenantRef, snapshotFor(1, 0))
		}
		maxOutput := 32
		request := &interaction.UnifiedRequest{Kind: interaction.RequestChat, Model: "default-chat", Chat: &interaction.ChatPayload{Messages: []interaction.Message{{Role: "user", Content: "hello"}}, MaxOutputTokens: &maxOutput}}
		requestContext, err := admissionService.Admit(context.Background(), admission.Input{Token: token.String(), BodyBytes: 32, Protocol: "openai", Request: request})
		if err != nil {
			t.Fatal(err)
		}
		plan, result, err := modelService.Invoke(context.Background(), gatewaymodel.Input{Snapshot: requestContext.Snapshot, Key: runtimeKeys[i], RequestID: requestContext.RequestID, ProjectID: projectID, RequiredCapabilities: []string{"chat"}, Request: request})
		if err != nil {
			t.Fatal(err)
		}
		selected[plan.Selected()]++
		usage := result.Response.Usage
		facts := accounting.Facts{UsageEventID: fmt.Sprintf("20000000-0000-4000-8000-%012d", i+1), RequestID: requestContext.RequestID, TenantID: tenantID, ProjectID: projectID, KeyID: runtimeKeys[i].ID, LogicalModel: "default-chat", DeploymentID: plan.Selected(), Outcome: "success", UsageSource: "api", InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, LatencyMS: 1, SnapshotVersion: requestContext.Snapshot.Version, ReceivedAt: requestContext.ReceivedAt, CompletedAt: time.Now().UTC()}
		if created, err := accountingRepo.Finalize(context.Background(), facts); err != nil || !created {
			t.Fatalf("Finalize()=%t,%v", created, err)
		}
		record, err := accountingRepo.GetRequest(context.Background(), tenancy.TenantScope{TenantID: tenantID}, requestContext.RequestID)
		if err != nil || record.LogicalModel != "default-chat" || record.DeploymentID != plan.Selected() || record.KeyID != runtimeKeys[i].ID || record.ProviderCost != nil || record.LatencyMS != 1 {
			t.Fatalf("record=%+v err=%v", record, err)
		}
	}
	if selected["open"] != 5 || selected["anthropic"] != 5 {
		t.Fatalf("selected=%v", selected)
	}
	if time.Since(started) >= 5*time.Minute {
		t.Fatalf("first-call scenario exceeded five minutes")
	}
	var requests, usage int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_records`).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_events`).Scan(&usage); err != nil {
		t.Fatal(err)
	}
	if requests != 10 || usage != 10 {
		t.Fatalf("facts requests=%d usage=%d", requests, usage)
	}
}
