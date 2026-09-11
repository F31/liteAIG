package credentialpool

import (
	"errors"

	"github.com/F31/liteAIG/internal/kernel/runtime"
)

// SnapshotState derives eligibility from a compiled Tenant Runtime Snapshot and
// delegates inflight/cost/quota to optional runtime hooks.
type SnapshotState struct {
	snapshot     *runtime.TenantRuntimeSnapshot
	inflight     func(string) int
	cost         func(string) float64
	quota        func(string) (float64, float64)
	circuitOpen  func(deploymentID, credentialID string) bool
	deploymentID string
}

// NewSnapshotState builds a pool state over a snapshot with optional hooks.
func NewSnapshotState(snapshot *runtime.TenantRuntimeSnapshot, deploymentID string) *SnapshotState {
	return &SnapshotState{snapshot: snapshot, deploymentID: deploymentID}
}

// WithInflight attaches an inflight observer.
func (s *SnapshotState) WithInflight(fn func(string) int) *SnapshotState { s.inflight = fn; return s }

// WithCost attaches a per-credential cost observer.
func (s *SnapshotState) WithCost(fn func(string) float64) *SnapshotState { s.cost = fn; return s }

// WithQuota attaches a used/limit observer.
func (s *SnapshotState) WithQuota(fn func(string) (float64, float64)) *SnapshotState {
	s.quota = fn
	return s
}

// WithCircuit attaches a circuit view keyed by deployment+credential.
func (s *SnapshotState) WithCircuit(fn func(deploymentID, credentialID string) bool) *SnapshotState {
	s.circuitOpen = fn
	return s
}

// Eligible reports enabled credentials whose provider is enabled and whose
// circuit is not open.
func (s *SnapshotState) Eligible(credentialID string) bool {
	credential, ok := s.snapshot.Credential(credentialID)
	if !ok || credential.Status != "enabled" {
		return false
	}
	provider, ok := s.snapshot.Provider(credential.ProviderID)
	if !ok || provider.Status != "enabled" {
		return false
	}
	if s.circuitOpen != nil && s.circuitOpen(s.deploymentID, credentialID) {
		return false
	}
	return true
}

func (s *SnapshotState) Inflight(credentialID string) int {
	if s.inflight == nil {
		return 0
	}
	return s.inflight(credentialID)
}

func (s *SnapshotState) Cost(credentialID string) float64 {
	if s.cost == nil {
		return 0
	}
	return s.cost(credentialID)
}

func (s *SnapshotState) Quota(credentialID string) (float64, float64) {
	if s.quota == nil {
		return 0, 0
	}
	return s.quota(credentialID)
}

// FromSnapshot builds a Pool for a deployment from the compiled snapshot.
func FromSnapshot(snapshot *runtime.TenantRuntimeSnapshot, deployment runtime.Deployment, state State) (*Pool, error) {
	if deployment.PoolID != "" {
		pool, ok := snapshot.CredentialPool(deployment.PoolID)
		if !ok {
			return nil, errors.New("credential pool not found in snapshot")
		}
		members := make([]Member, 0, len(pool.Members))
		for _, member := range pool.Members {
			members = append(members, Member{CredentialID: member.CredentialID, Weight: member.Weight})
		}
		return New(Strategy(pool.Strategy), members, state)
	}
	if deployment.CredentialID == "" {
		return nil, errors.New("deployment has neither pool nor credential")
	}
	return New(RoundRobin, []Member{{CredentialID: deployment.CredentialID}}, state)
}

var _ State = (*SnapshotState)(nil)
