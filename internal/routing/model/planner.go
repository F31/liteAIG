// Package model builds explainable deterministic model route plans.
package model

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

var ErrNoEligibleDeployment = errors.New("no eligible deployment")

type CircuitView interface {
	Open(deploymentID, credentialID string) bool
}
type Input struct {
	Snapshot                                       *runtime.TenantRuntimeSnapshot
	Key                                            runtime.APIKey
	LogicalModel, ProjectID, RequestID, RoutingKey string
	RequiredCapabilities                           []string
	// RequiresTools marks function-calling requests; a deployment that
	// declares capabilities but not "tools" is excluded with
	// tools_not_supported. Deployments without declared capabilities are
	// unaffected (undeclared means "not modeled", not "unsupported").
	RequiresTools bool
	ContextTokens int
	Health        map[string]bool
	Circuit       CircuitView
	Metrics       map[string]DeploymentMetrics
}

// DeploymentMetrics carries soft-scoring inputs observed per deployment.
type DeploymentMetrics struct {
	LatencyMS     float64 // lower is better
	Cost          float64 // lower is better
	Load          float64 // lower is better
	CacheAffinity float64 // higher is better
}
type Exclusion struct{ Code string }
type CandidateEvidence struct {
	DeploymentID     string
	Eligible         bool
	Exclusions       []Exclusion
	Priority, Weight int
	SelectionScore   float64
	ScoreBreakdown   map[string]float64
}
type RoutePlan struct {
	tenantSnapshotVersion, policyVersion  int64
	logicalModel, routePolicyID, selected string
	fallback                              []string
	evidence                              []CandidateEvidence
}

func (p *RoutePlan) TenantSnapshotVersion() int64 { return p.tenantSnapshotVersion }
func (p *RoutePlan) PolicyVersion() int64         { return p.policyVersion }
func (p *RoutePlan) LogicalModel() string         { return p.logicalModel }
func (p *RoutePlan) RoutePolicyID() string        { return p.routePolicyID }
func (p *RoutePlan) Selected() string             { return p.selected }
func (p *RoutePlan) Fallback() []string           { return append([]string(nil), p.fallback...) }
func (p *RoutePlan) Evidence() []CandidateEvidence {
	result := make([]CandidateEvidence, len(p.evidence))
	copy(result, p.evidence)
	for i := range result {
		result[i].Exclusions = append([]Exclusion(nil), result[i].Exclusions...)
	}
	return result
}

type Planner struct{ roundRobin sync.Map }
type counter struct{ value atomic.Uint64 }

func (p *Planner) Plan(input Input) (*RoutePlan, error) {
	if input.Snapshot == nil {
		return nil, ErrNoEligibleDeployment
	}
	logical, ok := input.Snapshot.LogicalModel(input.LogicalModel)
	if !ok || !allowedModel(input.Key.ModelAllowlist, input.LogicalModel) {
		return nil, ErrNoEligibleDeployment
	}
	policy, ok := input.Snapshot.RoutePolicy(logical.RoutePolicyID)
	if !ok || policy.ProjectID != "" && policy.ProjectID != input.ProjectID {
		return nil, ErrNoEligibleDeployment
	}
	project, projectOK := input.Snapshot.Project(input.ProjectID)
	var evidence []CandidateEvidence
	var eligible []runtime.Deployment
	for _, id := range policy.DeploymentIDs {
		item := CandidateEvidence{DeploymentID: id, Weight: policy.Weights[id]}
		deployment, exists := input.Snapshot.Deployment(id)
		if !exists {
			item.Exclusions = append(item.Exclusions, Exclusion{"deployment_missing"})
			evidence = append(evidence, item)
			continue
		}
		item.Priority = deployment.Priority
		if item.Weight <= 0 {
			item.Weight = 1
		}
		if deployment.Status != "enabled" {
			item.Exclusions = append(item.Exclusions, Exclusion{"deployment_disabled"})
		}
		provider, providerOK := input.Snapshot.Provider(deployment.ProviderID)
		if !providerOK || provider.Status != "enabled" {
			item.Exclusions = append(item.Exclusions, Exclusion{"provider_unavailable"})
		}
		if deployment.PoolID != "" {
			pool, poolOK := input.Snapshot.CredentialPool(deployment.PoolID)
			available := false
			if poolOK {
				for _, member := range pool.Members {
					credential, ok := input.Snapshot.Credential(member.CredentialID)
					if ok && credential.Status == "enabled" {
						available = true
						break
					}
				}
			}
			if !available {
				item.Exclusions = append(item.Exclusions, Exclusion{"credential_pool_unavailable"})
			}
		} else {
			credential, credentialOK := input.Snapshot.Credential(deployment.CredentialID)
			if !credentialOK || credential.Status != "enabled" {
				item.Exclusions = append(item.Exclusions, Exclusion{"credential_unavailable"})
			}
		}
		if !hasCapabilities(deployment.Capabilities, input.RequiredCapabilities) {
			item.Exclusions = append(item.Exclusions, Exclusion{"capability_mismatch"})
		}
		if input.RequiresTools && len(deployment.Capabilities) > 0 && !contains(deployment.Capabilities, "tools") {
			item.Exclusions = append(item.Exclusions, Exclusion{"tools_not_supported"})
		}
		if deployment.ContextWindow > 0 && input.ContextTokens > deployment.ContextWindow {
			item.Exclusions = append(item.Exclusions, Exclusion{"context_window_exceeded"})
		}
		if projectOK && project.Status != "active" {
			item.Exclusions = append(item.Exclusions, Exclusion{"project_disabled"})
		}
		if !projectOK {
			item.Exclusions = append(item.Exclusions, Exclusion{"project_missing"})
		}
		if projectOK && project.ResidencyEnforcement == "strict" && !contains(project.AllowedDataRegions, deployment.DataRegion) {
			item.Exclusions = append(item.Exclusions, Exclusion{"data_residency_mismatch"})
		}
		if healthy, known := input.Health[id]; known && !healthy {
			item.Exclusions = append(item.Exclusions, Exclusion{"unhealthy"})
		}
		if input.Circuit != nil && input.Circuit.Open(id, deployment.CredentialID) {
			item.Exclusions = append(item.Exclusions, Exclusion{"circuit_open"})
		}
		item.Eligible = len(item.Exclusions) == 0
		if item.Eligible {
			eligible = append(eligible, deployment)
		}
		evidence = append(evidence, item)
	}
	if len(eligible) == 0 {
		return &RoutePlan{tenantSnapshotVersion: input.Snapshot.Version, policyVersion: policy.Version, logicalModel: input.LogicalModel, routePolicyID: policy.ID, evidence: evidence}, ErrNoEligibleDeployment
	}
	order, scores, breakdowns := p.order(policy, input, eligible)
	for i := range evidence {
		evidence[i].SelectionScore = scores[evidence[i].DeploymentID]
		evidence[i].ScoreBreakdown = breakdowns[evidence[i].DeploymentID]
	}
	return &RoutePlan{tenantSnapshotVersion: input.Snapshot.Version, policyVersion: policy.Version, logicalModel: input.LogicalModel, routePolicyID: policy.ID, selected: order[0], fallback: order, evidence: evidence}, nil
}
func (p *Planner) order(policy runtime.RoutePolicy, input Input, items []runtime.Deployment) ([]string, map[string]float64, map[string]map[string]float64) {
	scores := map[string]float64{}
	breaks := map[string]map[string]float64{}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	switch policy.Strategy {
	case "weighted":
		key := input.RoutingKey
		if key == "" {
			key = input.RequestID
		}
		for _, item := range items {
			scores[item.ID] = weightedScore(key, item.ID, max(policy.Weights[item.ID], 1))
		}
		sort.SliceStable(items, func(i, j int) bool { return scores[items[i].ID] < scores[items[j].ID] })
	case "round_robin":
		key := input.Snapshot.TenantRef + ":" + policy.ID + ":" + strconv.FormatInt(input.Snapshot.Version, 10)
		value, _ := p.roundRobin.LoadOrStore(key, &counter{})
		start := int(value.(*counter).value.Add(1)-1) % len(items)
		items = append(items[start:], items[:start]...)
	case "soft":
		scores, breaks = softScores(policy, items, input.Metrics)
		sort.SliceStable(items, func(i, j int) bool { return scores[items[i].ID] < scores[items[j].ID] })
	default:
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Priority == items[j].Priority {
				return items[i].ID < items[j].ID
			}
			return items[i].Priority < items[j].Priority
		})
	}
	result := make([]string, len(items))
	for i, item := range items {
		result[i] = item.ID
	}
	return result, scores, breaks
}

// defaultScoreWeights returns explicit defaults for the soft score when none
// are configured (latency and cost equally weighted).
func defaultScoreWeights(configured map[string]float64) map[string]float64 {
	weights := map[string]float64{"latency": 1, "cost": 1, "load": 0, "cache": 0}
	if len(configured) > 0 {
		weights = configured
	}
	return weights
}

// softScores computes a bounded normalized weighted sum per deployment. Each
// metric is normalized against the observed maximum with a noise floor so small
// absolute differences do not dominate, and lower total scores are preferred.
func softScores(policy runtime.RoutePolicy, items []runtime.Deployment, metrics map[string]DeploymentMetrics) (map[string]float64, map[string]map[string]float64) {
	weights := defaultScoreWeights(policy.ScoreWeights)
	latencyMax, costMax, loadMax, affinityMax := 0.0, 0.0, 0.0, 0.0
	for _, item := range items {
		metric := metrics[item.ID]
		latencyMax = math.Max(latencyMax, metric.LatencyMS)
		costMax = math.Max(costMax, metric.Cost)
		loadMax = math.Max(loadMax, metric.Load)
		affinityMax = math.Max(affinityMax, metric.CacheAffinity)
	}
	const floor = 0.001
	latencyScale := math.Max(latencyMax, floor)
	costScale := math.Max(costMax, floor)
	loadScale := math.Max(loadMax, floor)
	affinityScale := math.Max(affinityMax, floor)

	scores := map[string]float64{}
	breaks := map[string]map[string]float64{}
	for _, item := range items {
		metric := metrics[item.ID]
		latencyComponent := math.Min(metric.LatencyMS/latencyScale, 1)
		costComponent := math.Min(metric.Cost/costScale, 1)
		loadComponent := math.Min(metric.Load/loadScale, 1)
		affinityComponent := math.Min(metric.CacheAffinity/affinityScale, 1)
		total := weights["latency"]*latencyComponent +
			weights["cost"]*costComponent +
			weights["load"]*loadComponent +
			weights["cache"]*(1-affinityComponent)
		scores[item.ID] = total
		breaks[item.ID] = map[string]float64{
			"latency": latencyComponent, "cost": costComponent,
			"load": loadComponent, "cache_affinity": affinityComponent,
		}
	}
	return scores, breaks
}
func weightedScore(key, id string, weight int) float64 {
	sum := sha256.Sum256([]byte(key + "\x00" + id))
	value := binary.BigEndian.Uint64(sum[:8])
	unit := (float64(value) + 1) / (float64(^uint64(0)) + 1)
	return -math.Log(unit) / float64(weight)
}
func allowedModel(allowlist []string, model string) bool {
	return len(allowlist) == 0 || contains(allowlist, model)
}
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func hasCapabilities(actual, required []string) bool {
	for _, value := range required {
		if !contains(actual, value) {
			return false
		}
	}
	return true
}
