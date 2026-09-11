// Package runtime defines immutable views consumed by the Data Plane.
package runtime

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// GlobalRuntime contains platform-level configuration visible to the Data Plane.
type GlobalRuntime struct {
	Version     int64
	PublishedAt time.Time
	pepperRefs  map[int]string
	drain       DrainConfig
	secretCache SecretCacheConfig
}

type GlobalRuntimeData struct {
	Version     int64
	PublishedAt time.Time
	PepperRefs  map[int]string
	Drain       DrainConfig
	SecretCache SecretCacheConfig
}

func NewGlobalRuntime(data GlobalRuntimeData) *GlobalRuntime {
	refs := make(map[int]string, len(data.PepperRefs))
	for k, v := range data.PepperRefs {
		refs[k] = v
	}
	return &GlobalRuntime{Version: data.Version, PublishedAt: data.PublishedAt, pepperRefs: refs, drain: data.Drain, secretCache: data.SecretCache}
}
func (g *GlobalRuntime) PepperRef(version int) (string, bool) {
	value, ok := g.pepperRefs[version]
	return value, ok
}

// DrainConfig is the process-level graceful drain timeout configuration.
type DrainConfig struct {
	DrainTimeoutMS         int64
	StreamDrainTimeoutMS   int64
	ForceShutdownTimeoutMS int64
}

// SecretCacheConfig is the process-level secret provider cache configuration.
type SecretCacheConfig struct {
	TTLMS             int64
	StaleGraceMS      int64
	RotationOverlapMS int64
}

// Drain returns the compiled drain configuration (zero value when unset).
func (g *GlobalRuntime) Drain() DrainConfig { return g.drain }

// SecretCache returns the compiled secret cache configuration.
func (g *GlobalRuntime) SecretCache() SecretCacheConfig { return g.secretCache }

// TenantRuntimeSnapshot is the immutable tenant configuration captured by a request.
// Compiled indexes are added by their owning domain compiler tasks.
type TenantRuntimeSnapshot struct {
	TenantID        string
	TenantRef       string
	Status          string
	Version         int64
	SecurityEpoch   int64
	PublishedAt     time.Time
	projects        map[string]Project
	providers       map[string]Provider
	credentials     map[string]Credential
	deployments     map[string]Deployment
	routePolicies   map[string]RoutePolicy
	logicalModels   map[string]LogicalModel
	apiKeys         map[string]APIKey
	budgetPolicies  map[string]BudgetPolicy
	credentialPools map[string]CredentialPool
	leases          map[string]LeaseConfig
	circuit         CircuitConfig
	drift           DriftMetadata
	spool           SpoolPolicy
	cache           CachePolicy
	mcpServers      map[string]MCPServer
	tools           map[string]Tool
	toolPolicies    map[string]ToolPolicy
	agents          map[string]Agent
	agentEndpoints  map[string]AgentEndpoint
	capabilityIndex map[string][]string // capability -> agent ids
	federatedAgents map[string]FederatedAgent
	federatedRels   map[string]FederationRelationship
	guardrail       GuardrailPolicy
	sourceData      TenantSnapshotData
}

// Data returns the compiled snapshot data this runtime was built from. It is
// the serializable record persisted as the tenant's Last Known Good bundle.
func (s *TenantRuntimeSnapshot) Data() TenantSnapshotData {
	return s.sourceData
}

type Project struct {
	ID                   string
	Status               string
	AllowedDataRegions   []string
	ResidencyEnforcement string
}

type Provider struct {
	ID, OwnerScope, Type, Endpoint, Status string
}
type Credential struct{ ID, ProviderID, OwnerScope, SecretRef, Status string }
type Deployment struct {
	ID, ProviderID, CredentialID, UpstreamModel, DataRegion, Status string
	PoolID                                                          string
	Capabilities                                                    []string
	ContextWindow, Priority                                         int
}
type RoutePolicy struct {
	ID, ProjectID, Strategy string
	DeploymentIDs           []string
	Weights                 map[string]int
	ScoreWeights            map[string]float64
	Version                 int64
}
type LogicalModel struct{ ID, Alias, RoutePolicyID string }
type APIKey struct {
	ID, PublicID, ProjectID, ApplicationID, AgentID, ServiceAccountID, Status string
	HMACDigest                                                                []byte
	PepperVersion                                                             int
	ExpiresAt                                                                 *time.Time
	ModelAllowlist, IPAllowlist                                               []string
}

// BudgetPolicy is one compiled budget window scope (Phase 1 distributed ledger).
type BudgetPolicy struct {
	ID          string
	TenantID    string
	ProjectID   string // empty for tenant-scope windows
	KeyID       string // empty unless key-scoped
	WindowHours int64
	TokenLimit  float64
	Mode        string // hard|soft
	Consistency string // regional|global_soft|global_hard
}

// SpoolPolicy is the compiled per-Tenant accounting durability policy.
type SpoolPolicy struct {
	Mode              string // hard|soft
	WarnThreshold     float64
	CriticalThreshold float64
	HardThreshold     float64
	QuotaBytes        int64
}

// CachePolicy is the compiled Exact Cache policy.
type CachePolicy struct {
	Enabled          bool
	TTLSeconds       int64
	NamespaceVersion int64
	AllowedKinds     []string
	MaxTemperature   float64
	Semantic         SemanticCachePolicy
}

// SemanticCachePolicy is the compiled semantic-cache (similarity lookup)
// policy. Embeddings are produced by the tenant's own provider.
type SemanticCachePolicy struct {
	Enabled   bool
	Model     string
	Threshold float64
}

type MCPServer struct{ ID, URL, Status string }
type Tool struct {
	ID, ServerID, Name, DataClassification, Endpoint, Status string
	Schema                                                   []byte
	CapabilityTags                                           []string
}
type ToolPolicy struct {
	ToolID, ProjectID string
	AllowedAgentIDs   []string
	Allowed           bool
	RequireApproval   bool
}
type Agent struct {
	ID, ProjectID, Name, Status, CurrentVersion string
	Capabilities                                []string
	RequireApproval                             bool
}

// AgentEndpoint is one versioned endpoint of an agent.
type AgentEndpoint struct {
	ID, AgentID, Version, URL, Protocol string
	AuthScheme, SecretRef               string
	Headers                             map[string]string
	DataClassification                  string
	Capabilities                        []string
}

// FederatedAgent is a tenant-scoped projection of an external agent.
type FederatedAgent struct {
	ID, Name, ExternalSubject, Status string
	TrustBoundary                     string // internal|external_federated
}

// FederationRelationship is the compiled trust relationship summary. It also
// carries the governance facts the outbound A2A gate evaluates: direction,
// project and capability grants, data boundary, and the pinned approved
// endpoint version.
type FederationRelationship struct {
	ID, ExternalAgentID, Status, AssuranceLevel string
	HasVerifiedAnchor                           bool
	Direction                                   string
	ProjectGrants                               []string
	CapabilityGrants                            []string
	BoundaryStatus                              string
	ProcessingRegions                           []string
	ApprovedVersion                             string
}

// GuardrailRule mirrors a guardrail policy rule without coupling the Kernel to
// the guardrail package.
type GuardrailRule struct {
	ID, Kind, Pattern, Action, Replacement string
}

// GuardrailPolicy is the compiled guardrail policy for a Tenant.
type GuardrailPolicy struct {
	Mode          string
	SecurityEpoch int64
	Rules         []GuardrailRule
	Judge         GuardrailJudge
	Groundedness  GuardrailGroundedness
}

// GuardrailJudge is the compiled LLM-as-Judge output checkpoint.
type GuardrailJudge struct {
	Enabled bool
	Model   string
	Action  string // block|mark
}

// GuardrailGroundedness is the compiled groundedness output checkpoint.
type GuardrailGroundedness struct {
	Enabled    bool
	MinOverlap float64
}

// CredentialPool is a compiled pool over credentials with a selection strategy.
type CredentialPool struct {
	ID         string
	ProviderID string
	TenantID   string
	Strategy   string // round_robin|weighted|least_inflight|quota_aware
	Members    []PoolMember
}

// PoolMember is one credential with an optional weight.
type PoolMember struct {
	CredentialID string
	Weight       int
}

// LeaseConfig is the compiled concurrency lease capacity for a scope.
type LeaseConfig struct {
	DeploymentID string
	Capacity     int
	TTLSeconds   int64
}

// CircuitConfig is the compiled full-circuit-state-machine parameters.
type CircuitConfig struct {
	MinSamples        int
	ErrorRate         float64
	InitialCooldownMS int64
	MaxCooldownMS     int64
	CooldownFactor    float64
}

// DriftMetadata carries drift detection expectations for readiness.
type DriftMetadata struct {
	Version       int64
	GraceMS       int64
	Strict        bool
	SecurityEpoch int64
}

type TenantSnapshotData struct {
	TenantID, TenantRef, Status string
	Version, SecurityEpoch      int64
	PublishedAt                 time.Time
	Projects                    []Project
	Providers                   []Provider
	Credentials                 []Credential
	Deployments                 []Deployment
	RoutePolicies               []RoutePolicy
	LogicalModels               []LogicalModel
	APIKeys                     []APIKey
	BudgetPolicies              []BudgetPolicy
	CredentialPools             []CredentialPool
	Leases                      []LeaseConfig
	Circuit                     CircuitConfig
	Drift                       DriftMetadata
	Spool                       SpoolPolicy
	Cache                       CachePolicy
	MCPServers                  []MCPServer
	Tools                       []Tool
	ToolPolicies                []ToolPolicy
	Agents                      []Agent
	AgentEndpoints              []AgentEndpoint
	FederatedAgents             []FederatedAgent
	FederationRelationships     []FederationRelationship
	Guardrail                   GuardrailPolicy
}

// The compiled maps are shared by every request that captured the snapshot,
// so getters return deep copies. Each record type with mutable fields has
// exactly one clone function, shared by the constructor and the getters.

func cloneStrings(values []string) []string { return append([]string(nil), values...) }
func cloneBytes(values []byte) []byte       { return append([]byte(nil), values...) }

func cloneProject(value Project) Project {
	value.AllowedDataRegions = cloneStrings(value.AllowedDataRegions)
	return value
}

func cloneDeployment(value Deployment) Deployment {
	value.Capabilities = cloneStrings(value.Capabilities)
	return value
}

func cloneRoutePolicy(value RoutePolicy) RoutePolicy {
	value.DeploymentIDs = cloneStrings(value.DeploymentIDs)
	value.Weights = cloneStringIntMap(value.Weights)
	value.ScoreWeights = cloneStringFloatMap(value.ScoreWeights)
	return value
}

func cloneAPIKey(value APIKey) APIKey {
	value.HMACDigest = cloneBytes(value.HMACDigest)
	value.ModelAllowlist = cloneStrings(value.ModelAllowlist)
	value.IPAllowlist = cloneStrings(value.IPAllowlist)
	if value.ExpiresAt != nil {
		expiresAt := *value.ExpiresAt
		value.ExpiresAt = &expiresAt
	}
	return value
}

func cloneCredentialPool(value CredentialPool) CredentialPool {
	value.Members = append([]PoolMember(nil), value.Members...)
	return value
}

func cloneTool(value Tool) Tool {
	value.Schema = cloneBytes(value.Schema)
	value.CapabilityTags = cloneStrings(value.CapabilityTags)
	return value
}

func cloneToolPolicy(value ToolPolicy) ToolPolicy {
	value.AllowedAgentIDs = cloneStrings(value.AllowedAgentIDs)
	return value
}

func cloneAgent(value Agent) Agent {
	value.Capabilities = cloneStrings(value.Capabilities)
	return value
}

func cloneAgentEndpoint(value AgentEndpoint) AgentEndpoint {
	value.Capabilities = cloneStrings(value.Capabilities)
	value.Headers = cloneStringMap(value.Headers)
	return value
}

func cloneFederationRelationship(value FederationRelationship) FederationRelationship {
	value.ProjectGrants = cloneStrings(value.ProjectGrants)
	value.CapabilityGrants = cloneStrings(value.CapabilityGrants)
	value.ProcessingRegions = cloneStrings(value.ProcessingRegions)
	return value
}

func cloneCachePolicy(value CachePolicy) CachePolicy {
	value.AllowedKinds = cloneStrings(value.AllowedKinds)
	return value
}

func cloneGuardrailPolicy(value GuardrailPolicy) GuardrailPolicy {
	value.Rules = append([]GuardrailRule(nil), value.Rules...)
	return value
}

// identity is the clone function for value-only record types.
func identity[T any](value T) T { return value }

// snapshotCollect copies every stored value through clone.
func snapshotCollect[T any](stored map[string]T, clone func(T) T) []T {
	result := make([]T, 0, len(stored))
	for _, item := range stored {
		result = append(result, clone(item))
	}
	return result
}

// snapshotCollectSorted is snapshotCollect with deterministic ordering.
func snapshotCollectSorted[T any](stored map[string]T, clone func(T) T, less func(a, b T) bool) []T {
	result := snapshotCollect(stored, clone)
	sort.Slice(result, func(i, j int) bool { return less(result[i], result[j]) })
	return result
}

func NewTenantSnapshot(data TenantSnapshotData) *TenantRuntimeSnapshot {
	snapshot := &TenantRuntimeSnapshot{
		TenantID: data.TenantID, TenantRef: data.TenantRef, Status: data.Status, Version: data.Version,
		SecurityEpoch: data.SecurityEpoch, PublishedAt: data.PublishedAt,
		projects: make(map[string]Project, len(data.Projects)), providers: make(map[string]Provider, len(data.Providers)),
		credentials: make(map[string]Credential, len(data.Credentials)), deployments: make(map[string]Deployment, len(data.Deployments)),
		routePolicies: make(map[string]RoutePolicy, len(data.RoutePolicies)), logicalModels: make(map[string]LogicalModel, len(data.LogicalModels)),
		apiKeys:         make(map[string]APIKey, len(data.APIKeys)),
		budgetPolicies:  make(map[string]BudgetPolicy, len(data.BudgetPolicies)),
		credentialPools: make(map[string]CredentialPool, len(data.CredentialPools)),
		leases:          make(map[string]LeaseConfig, len(data.Leases)),
		circuit:         data.Circuit,
		drift:           data.Drift,
		spool:           data.Spool,
		cache:           cloneCachePolicy(data.Cache),
		mcpServers:      make(map[string]MCPServer, len(data.MCPServers)),
		tools:           make(map[string]Tool, len(data.Tools)),
		toolPolicies:    make(map[string]ToolPolicy, len(data.ToolPolicies)),
		agents:          make(map[string]Agent, len(data.Agents)),
		agentEndpoints:  make(map[string]AgentEndpoint, len(data.AgentEndpoints)),
		capabilityIndex: map[string][]string{},
		federatedAgents: make(map[string]FederatedAgent, len(data.FederatedAgents)),
		federatedRels:   make(map[string]FederationRelationship, len(data.FederationRelationships)),
		sourceData:      data,
	}
	for _, item := range data.Projects {
		snapshot.projects[item.ID] = cloneProject(item)
	}
	for _, item := range data.Providers {
		snapshot.providers[item.ID] = item
	}
	for _, item := range data.Credentials {
		snapshot.credentials[item.ID] = item
	}
	for _, item := range data.Deployments {
		snapshot.deployments[item.ID] = cloneDeployment(item)
	}
	for _, item := range data.RoutePolicies {
		snapshot.routePolicies[item.ID] = cloneRoutePolicy(item)
	}
	for _, item := range data.LogicalModels {
		snapshot.logicalModels[item.Alias] = item
	}
	for _, item := range data.APIKeys {
		snapshot.apiKeys[item.PublicID] = cloneAPIKey(item)
	}
	for _, item := range data.BudgetPolicies {
		snapshot.budgetPolicies[item.ID] = item
	}
	for _, item := range data.CredentialPools {
		snapshot.credentialPools[item.ID] = cloneCredentialPool(item)
	}
	for _, item := range data.Leases {
		snapshot.leases[item.DeploymentID] = item
	}
	for _, item := range data.MCPServers {
		snapshot.mcpServers[item.ID] = item
	}
	for _, item := range data.Tools {
		snapshot.tools[item.ID] = cloneTool(item)
	}
	for _, item := range data.ToolPolicies {
		snapshot.toolPolicies[item.ProjectID+":"+item.ToolID] = cloneToolPolicy(item)
	}
	for _, item := range data.Agents {
		snapshot.agents[item.ID] = cloneAgent(item)
		snapshot.indexCapabilities(item.ID, item.Capabilities)
	}
	for _, item := range data.AgentEndpoints {
		snapshot.agentEndpoints[item.ID] = cloneAgentEndpoint(item)
		snapshot.indexCapabilities(item.AgentID, item.Capabilities)
	}
	snapshot.guardrail = cloneGuardrailPolicy(data.Guardrail)
	for _, item := range data.FederatedAgents {
		snapshot.federatedAgents[item.ID] = item
	}
	for _, item := range data.FederationRelationships {
		snapshot.federatedRels[item.ID] = cloneFederationRelationship(item)
	}
	return snapshot
}

// FederatedAgent returns a compiled federated agent projection.
func (s *TenantRuntimeSnapshot) FederatedAgent(id string) (FederatedAgent, bool) {
	value, ok := s.federatedAgents[id]
	return value, ok
}

// FederatedAgents returns all compiled federated agent projections.
func (s *TenantRuntimeSnapshot) FederatedAgents() []FederatedAgent {
	return snapshotCollect(s.federatedAgents, identity[FederatedAgent])
}

// FederationRelationship returns a compiled relationship summary.
func (s *TenantRuntimeSnapshot) FederationRelationship(id string) (FederationRelationship, bool) {
	value, ok := s.federatedRels[id]
	return cloneFederationRelationship(value), ok
}

// FederationRelationships returns all compiled relationship summaries.
func (s *TenantRuntimeSnapshot) FederationRelationships() []FederationRelationship {
	return snapshotCollect(s.federatedRels, cloneFederationRelationship)
}

func (s *TenantRuntimeSnapshot) indexCapabilities(agentID string, capabilities []string) {
	for _, capability := range capabilities {
		existing := s.capabilityIndex[capability]
		seen := false
		for _, id := range existing {
			if id == agentID {
				seen = true
				break
			}
		}
		if !seen {
			s.capabilityIndex[capability] = append(existing, agentID)
		}
	}
}

func cloneStringFloatMap(input map[string]float64) map[string]float64 {
	if input == nil {
		return nil
	}
	output := make(map[string]float64, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
func cloneStringIntMap(input map[string]int) map[string]int {
	if input == nil {
		return nil
	}
	output := make(map[string]int, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

// Single-item lookups return deep copies so callers can never mutate a
// value shared with the compiled snapshot.

// AgentsByCapability returns agent ids that expose a capability.
func (s *TenantRuntimeSnapshot) AgentsByCapability(capability string) []string {
	return cloneStrings(s.capabilityIndex[capability])
}

// AgentEndpoint returns a versioned endpoint by id.
func (s *TenantRuntimeSnapshot) AgentEndpoint(id string) (AgentEndpoint, bool) {
	value, ok := s.agentEndpoints[id]
	return cloneAgentEndpoint(value), ok
}

// AgentEndpoints returns all compiled agent endpoints.
func (s *TenantRuntimeSnapshot) AgentEndpoints() []AgentEndpoint {
	return snapshotCollect(s.agentEndpoints, cloneAgentEndpoint)
}

func (s *TenantRuntimeSnapshot) Project(id string) (Project, bool) {
	value, ok := s.projects[id]
	return cloneProject(value), ok
}
func (s *TenantRuntimeSnapshot) Provider(id string) (Provider, bool) {
	value, ok := s.providers[id]
	return value, ok
}
func (s *TenantRuntimeSnapshot) Credential(id string) (Credential, bool) {
	value, ok := s.credentials[id]
	return value, ok
}
func (s *TenantRuntimeSnapshot) Deployment(id string) (Deployment, bool) {
	value, ok := s.deployments[id]
	return cloneDeployment(value), ok
}
func (s *TenantRuntimeSnapshot) RoutePolicy(id string) (RoutePolicy, bool) {
	value, ok := s.routePolicies[id]
	return cloneRoutePolicy(value), ok
}
func (s *TenantRuntimeSnapshot) LogicalModel(alias string) (LogicalModel, bool) {
	value, ok := s.logicalModels[alias]
	return value, ok
}
func (s *TenantRuntimeSnapshot) LogicalModels() []LogicalModel {
	return snapshotCollectSorted(s.logicalModels, identity[LogicalModel],
		func(a, b LogicalModel) bool { return a.Alias < b.Alias })
}
func (s *TenantRuntimeSnapshot) APIKey(publicID string) (APIKey, bool) {
	value, ok := s.apiKeys[publicID]
	return cloneAPIKey(value), ok
}

func (s *TenantRuntimeSnapshot) BudgetPolicy(id string) (BudgetPolicy, bool) {
	value, ok := s.budgetPolicies[id]
	return value, ok
}
func (s *TenantRuntimeSnapshot) BudgetPolicies() []BudgetPolicy {
	return snapshotCollectSorted(s.budgetPolicies, identity[BudgetPolicy],
		func(a, b BudgetPolicy) bool { return a.ID < b.ID })
}
func (s *TenantRuntimeSnapshot) CredentialPool(id string) (CredentialPool, bool) {
	value, ok := s.credentialPools[id]
	return cloneCredentialPool(value), ok
}
func (s *TenantRuntimeSnapshot) Lease(deploymentID string) (LeaseConfig, bool) {
	value, ok := s.leases[deploymentID]
	return value, ok
}
func (s *TenantRuntimeSnapshot) CircuitConfig() CircuitConfig { return s.circuit }
func (s *TenantRuntimeSnapshot) Drift() DriftMetadata         { return s.drift }
func (s *TenantRuntimeSnapshot) SpoolPolicy() SpoolPolicy     { return s.spool }
func (s *TenantRuntimeSnapshot) CachePolicy() CachePolicy     { return cloneCachePolicy(s.cache) }
func (s *TenantRuntimeSnapshot) Tool(id string) (Tool, bool) {
	value, ok := s.tools[id]
	return cloneTool(value), ok
}
func (s *TenantRuntimeSnapshot) ToolPolicy(projectID, toolID string) (ToolPolicy, bool) {
	value, ok := s.toolPolicies[projectID+":"+toolID]
	return cloneToolPolicy(value), ok
}
func (s *TenantRuntimeSnapshot) Agent(id string) (Agent, bool) {
	value, ok := s.agents[id]
	return cloneAgent(value), ok
}
func (s *TenantRuntimeSnapshot) Tools() []Tool {
	return snapshotCollect(s.tools, cloneTool)
}
func (s *TenantRuntimeSnapshot) MCPServers() []MCPServer {
	return snapshotCollect(s.mcpServers, identity[MCPServer])
}
func (s *TenantRuntimeSnapshot) Agents() []Agent {
	return snapshotCollect(s.agents, cloneAgent)
}

// GuardrailPolicy returns a copy of the compiled guardrail policy.
func (s *TenantRuntimeSnapshot) GuardrailPolicy() GuardrailPolicy {
	return cloneGuardrailPolicy(s.guardrail)
}

// Providers returns all compiled providers (control-plane/health surfaces).
func (s *TenantRuntimeSnapshot) Providers() []Provider {
	return snapshotCollect(s.providers, identity[Provider])
}

// Deployments returns all compiled deployments (control-plane/health surfaces).
func (s *TenantRuntimeSnapshot) Deployments() []Deployment {
	return snapshotCollect(s.deployments, cloneDeployment)
}

// Credentials returns all compiled credentials (control-plane surfaces).
func (s *TenantRuntimeSnapshot) Credentials() []Credential {
	return snapshotCollect(s.credentials, identity[Credential])
}

// Routes returns all compiled route policies (control-plane surfaces).
func (s *TenantRuntimeSnapshot) Routes() []RoutePolicy {
	return snapshotCollect(s.routePolicies, cloneRoutePolicy)
}

// Registry provides read-only access to active runtime views.
type Registry interface {
	Global() *GlobalRuntime
	Tenant(tenantRef string) (*TenantRuntimeSnapshot, bool)
}

type tenantSlot struct {
	pointer atomic.Pointer[TenantRuntimeSnapshot]
}

// ActiveRegistry atomically replaces only the affected Tenant snapshot.
type ActiveRegistry struct {
	global  atomic.Pointer[GlobalRuntime]
	tenants sync.Map
}

func (r *ActiveRegistry) Global() *GlobalRuntime              { return r.global.Load() }
func (r *ActiveRegistry) ActivateGlobal(value *GlobalRuntime) { r.global.Store(value) }
func (r *ActiveRegistry) Tenant(ref string) (*TenantRuntimeSnapshot, bool) {
	value, ok := r.tenants.Load(ref)
	if !ok {
		return nil, false
	}
	snapshot := value.(*tenantSlot).pointer.Load()
	return snapshot, snapshot != nil
}
func (r *ActiveRegistry) ActivateTenant(ref string, snapshot *TenantRuntimeSnapshot) {
	value, _ := r.tenants.LoadOrStore(ref, &tenantSlot{})
	value.(*tenantSlot).pointer.Store(snapshot)
}

// HasAnyTenant reports whether at least one tenant runtime snapshot is loaded.
// Readiness probes use it to refuse traffic before the data plane can serve.
func (r *ActiveRegistry) HasAnyTenant() bool {
	var found bool
	r.tenants.Range(func(_, value any) bool {
		snapshot := value.(*tenantSlot).pointer.Load()
		if snapshot != nil {
			found = true
			return false
		}
		return true
	})
	return found
}

// TenantByID finds the active snapshot for a tenant ID. It is O(N) and intended
// for control-plane surfaces (Health/Runtime), not the hot path.
func (r *ActiveRegistry) TenantByID(tenantID string) (*TenantRuntimeSnapshot, bool) {
	var found *TenantRuntimeSnapshot
	r.tenants.Range(func(_, value any) bool {
		snapshot := value.(*tenantSlot).pointer.Load()
		if snapshot != nil && snapshot.TenantID == tenantID {
			found = snapshot
			return false
		}
		return true
	})
	return found, found != nil
}

var _ Registry = (*ActiveRegistry)(nil)
