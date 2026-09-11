package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/F31/liteAIG/internal/access/protocol/a2a"
	"github.com/F31/liteAIG/internal/controlplane/approval"
	"github.com/F31/liteAIG/internal/controlplane/backend"
	"github.com/F31/liteAIG/internal/federation"
	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/observability/agentgraph"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/internal/tenancy"
)

// liteApprovals adapts the approval service to the admin Backend surfaces.
type liteApprovals struct {
	service *approval.Service
}

func (a *liteApprovals) List(ctx context.Context, scope tenancy.TenantScope, limit int) ([]backend.ApprovalView, error) {
	requests, err := a.service.List(ctx, scope, limit)
	if err != nil {
		return nil, err
	}
	views := make([]backend.ApprovalView, 0, len(requests))
	for _, request := range requests {
		views = append(views, backend.ApprovalView{
			ID: request.ID, Requester: request.Requester, Action: request.Action,
			Target: request.Target, Status: string(request.Status),
			Approvers: len(request.ApprovedBy), DualApproval: request.DualApproval,
		})
	}
	return views, nil
}

func (a *liteApprovals) Decide(ctx context.Context, scope tenancy.TenantScope, id, decision, actor string) error {
	switch decision {
	case "approve":
		_, err := a.service.Approve(ctx, scope, id, actor)
		return err
	case "reject":
		_, err := a.service.Reject(ctx, scope, id, actor)
		return err
	default:
		return errors.New("unknown approval decision")
	}
}

// liteAgentGraph rebuilds the agent/task graph from the accounting ledger.
type liteAgentGraph struct {
	accounting accounting.Repository
}

func (a *liteAgentGraph) Build(ctx context.Context, scope tenancy.TenantScope, rootTask string) (backend.AgentGraphView, error) {
	requests, err := a.accounting.ListRequests(ctx, scope, 500)
	if err != nil {
		return backend.AgentGraphView{}, err
	}
	graph := agentgraph.NewBuilder(scope.TenantID).Build(requests)
	view := backend.AgentGraphView{RootTask: graph.RootTask, TotalCost: graph.TotalCost()}
	for _, hop := range graph.Hops {
		view.Hops = append(view.Hops, backend.AgentGraphHopView{
			Order: hop.Order, AgentID: hop.AgentID, Model: hop.Model,
			Cost: hop.Cost, Outcome: hop.Outcome,
		})
	}
	if rootTask != "" && rootTask != "root" && rootTask != graph.RootTask {
		return backend.AgentGraphView{RootTask: rootTask}, nil
	}
	return view, nil
}

// liteFederation overlays lifecycle state (suspended/revoked) onto the
// snapshot-backed relationship view, suspends relationships, and closes the
// Agent Card discovery→verify→review→activate loop for federated peers.
type liteFederation struct {
	lifecycle  *federation.Lifecycle
	registry   *runtime.ActiveRegistry
	cardClient *a2a.Client
	pushOutbox *sqlrepo.A2APushOutboxStore
}

func (f *liteFederation) Overlay(ctx context.Context, scope tenancy.TenantScope, view backend.FederationView) (backend.FederationView, error) {
	// The snapshot-derived relationship list is the compiled config's baseline;
	// the lifecycle store is authoritative for every relationship an operator
	// has discovered or reviewed. Merge the two: snapshot rows carry liver state
	// wherever a lifecycle row exists, and any lifecycle row not in the snapshot
	// (e.g. a fresh discovery candidate) is appended so the admin view shows the
	// full discover -> review -> activate lifecycle.
	live, err := f.lifecycle.List(ctx, scope)
	if err != nil {
		return backend.FederationView{}, err
	}
	byID := make(map[string]federation.Relationship, len(live))
	for _, relationship := range live {
		byID[relationship.ID] = relationship
	}
	seen := make(map[string]bool, len(view.Relationships))
	merged := make([]backend.RelationshipView, 0, len(view.Relationships)+len(live))
	for _, item := range view.Relationships {
		if relationship, ok := byID[item.ID]; ok {
			item.Status = string(relationship.Status)
			item.AssuranceLevel = relationship.AssuranceLevel
			item.DataBoundary = string(relationship.DataBoundary.Status)
			item.HasVerifiedAnchor = relationship.HasVerifiedAnchor()
			item.ProjectGrants = len(relationship.ProjectGrants)
			item.CapabilityGrants = len(relationship.CapabilityGrants)
		}
		merged = append(merged, item)
		seen[item.ID] = true
	}
	for _, relationship := range live {
		if seen[relationship.ID] {
			continue
		}
		merged = append(merged, toRelationshipView(relationship))
		seen[relationship.ID] = true
	}
	view.Relationships = merged
	if f.pushOutbox != nil {
		summary, err := f.pushOutbox.Summary(ctx, scope.TenantID)
		if err != nil {
			return backend.FederationView{}, err
		}
		view.PushOutbox = backend.A2APushOutboxView{
			Pending: summary.Pending, Sending: summary.Sending, Delivered: summary.Delivered, Failed: summary.Failed,
			EarliestNextAttemptAt: summary.EarliestNextAttemptAt,
		}
	}
	return view, nil
}

func (f *liteFederation) Suspend(ctx context.Context, scope tenancy.TenantScope, id, _ string) error {
	_, err := f.lifecycle.Suspend(ctx, scope, id)
	return err
}

// PushDeliveries lists the tenant's most recent sanitized A2A push callback
// deliveries, optionally filtered by status ("pending", "sending", "delivered",
// "failed"; empty selects all). Callback URLs, bearer tokens, and payload
// bodies are never selected.
func (f *liteFederation) PushDeliveries(ctx context.Context, scope tenancy.TenantScope, status string, limit int) ([]backend.PushDeliveryView, error) {
	if f.pushOutbox == nil {
		return nil, errors.New("a2a push outbox is not configured")
	}
	failures, err := f.pushOutbox.ListDeliveries(ctx, scope.TenantID, status, limit)
	if err != nil {
		return nil, err
	}
	views := make([]backend.PushDeliveryView, 0, len(failures))
	for _, failure := range failures {
		views = append(views, backend.PushDeliveryView{
			ID: failure.ID, TaskID: failure.TaskID, Status: failure.Status,
			Attempts: failure.Attempts, MaxAttempts: failure.MaxAttempts,
			LastError: failure.LastError, NextAttemptAt: failure.NextAttemptAt,
			UpdatedAt: failure.UpdatedAt,
		})
	}
	return views, nil
}

// Discover fetches a peer Agent Card, verifies its signature against the
// supplied public keys, and creates (or re-reviews) the federation
// relationship. Discovery never implies trust: the relationship stays a
// candidate until review activates it with a verified anchor plus grants.
func (f *liteFederation) Discover(ctx context.Context, scope tenancy.TenantScope, input backend.FederationDiscoverInput) (backend.RelationshipView, error) {
	if f.cardClient == nil {
		return backend.RelationshipView{}, errors.New("agent card discovery is not configured")
	}
	card, err := f.cardClient.Fetch(ctx, input.URL)
	if err != nil {
		return backend.RelationshipView{}, err
	}
	keys, err := parseVerificationKeys(input.VerificationKeys)
	if err != nil {
		return backend.RelationshipView{}, err
	}
	verified := false
	if card.Signature != nil {
		verified = a2a.VerifyCardSignature(card, keys...) == nil
	}
	id := relationshipID(card.Name, input.URL)
	if existing, ok := f.lifecycle.Get(ctx, scope, id); ok {
		change := cardMaterialChange(existing, input.URL, card, verified)
		if change.RequiresReview() {
			if _, err := f.lifecycle.ReviewMaterialChange(ctx, scope, id, change); err != nil {
				return backend.RelationshipView{}, err
			}
		}
		updated, _ := f.lifecycle.Get(ctx, scope, id)
		return toRelationshipView(updated), nil
	}
	relationship := federation.Relationship{
		ID:               id,
		TenantID:         scope.TenantID,
		ExternalAgentID:  card.Name,
		ExternalSubject:  card.URL,
		Name:             firstNonEmpty(input.Name, card.Name),
		AssuranceLevel:   "declared",
		AgentCardSource:  input.URL,
		AuthMethod:       "jws",
		DataBoundary:     federation.DataBoundary{Status: federation.BoundaryUnknown},
		Anchors:          []federation.TrustAnchor{{ID: "card", Type: federation.AnchorJWS, Subject: card.Name, Verified: verified, VerifiedAt: time.Now().UTC()}},
		ProjectGrants:    projectGrants(input.ProjectGrants),
		CapabilityGrants: capabilityGrants(input.CapabilityGrants),
	}
	created, err := f.lifecycle.Discover(ctx, relationship)
	if err != nil {
		return backend.RelationshipView{}, err
	}
	return toRelationshipView(created), nil
}

// Review approves or rejects a discovered relationship. Approval merges any
// supplied grants and then requires a verified anchor plus project and
// capability grants before the relationship can activate.
func (f *liteFederation) Review(ctx context.Context, scope tenancy.TenantScope, id string, input backend.FederationReviewInput, _ string) error {
	relationship, ok := f.lifecycle.Get(ctx, scope, id)
	if !ok {
		return errors.New("relationship not found")
	}
	if !input.Approved {
		_, err := f.lifecycle.Review(ctx, relationship, false)
		return err
	}
	if len(input.ProjectGrants) > 0 {
		relationship.ProjectGrants = projectGrants(input.ProjectGrants)
	}
	if len(input.CapabilityGrants) > 0 {
		relationship.CapabilityGrants = capabilityGrants(input.CapabilityGrants)
	}
	if _, err := f.lifecycle.Activate(ctx, relationship); err != nil {
		if errors.Is(err, federation.ErrUnverified) {
			return errors.New("cannot activate: relationship has no verified trust anchor")
		}
		if errors.Is(err, federation.ErrNotGranted) {
			return errors.New("cannot activate: project and capability grants are required")
		}
		return err
	}
	return nil
}

func toRelationshipView(relationship federation.Relationship) backend.RelationshipView {
	return backend.RelationshipView{
		ID:                relationship.ID,
		ExternalAgentID:   relationship.ExternalAgentID,
		Status:            string(relationship.Status),
		AssuranceLevel:    relationship.AssuranceLevel,
		DataBoundary:      string(relationship.DataBoundary.Status),
		HasVerifiedAnchor: relationship.HasVerifiedAnchor(),
		ProjectGrants:     len(relationship.ProjectGrants),
		CapabilityGrants:  len(relationship.CapabilityGrants),
	}
}

// relationshipID derives a stable relationship id from the peer identity so
// re-discovery of the same agent finds the existing relationship.
func relationshipID(name, url string) string {
	seed := name
	if seed == "" {
		seed = url
	}
	return "rel-" + sha256hex(seed)[:16]
}

func sha256hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func projectGrants(ids []string) []federation.ProjectGrant {
	grants := make([]federation.ProjectGrant, 0, len(ids))
	for _, id := range ids {
		grants = append(grants, federation.ProjectGrant{ID: id, ProjectID: id, GrantedBy: "admin"})
	}
	return grants
}

func capabilityGrants(capabilities []string) []federation.CapabilityGrant {
	grants := make([]federation.CapabilityGrant, 0, len(capabilities))
	for _, capability := range capabilities {
		grants = append(grants, federation.CapabilityGrant{ID: capability, Capability: capability})
	}
	return grants
}

// cardMaterialChange detects Agent Card facts that require re-review on an
// existing relationship (see federation.MaterialChange field vocabulary).
func cardMaterialChange(existing federation.Relationship, cardURL string, card *a2a.AgentCard, verified bool) federation.MaterialChange {
	var fields []string
	if existing.AgentCardSource != "" && existing.AgentCardSource != cardURL {
		fields = append(fields, "endpoint")
	}
	if card != nil && card.Version != "" && existing.ApprovedVersion != "" && card.Version != existing.ApprovedVersion {
		fields = append(fields, "publisher")
	}
	if existing.HasVerifiedAnchor() != verified {
		fields = append(fields, "trust_key")
	}
	return federation.MaterialChange{Fields: fields}
}

// parseVerificationKeys decodes PEM public keys into a2a.PublicKey values,
// deriving the algorithm from the key type unless the caller pinned one.
func parseVerificationKeys(keys []backend.VerificationKey) ([]a2a.PublicKey, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	out := make([]a2a.PublicKey, 0, len(keys))
	for _, key := range keys {
		block, _ := pem.Decode([]byte(key.PEM))
		if block == nil {
			return nil, errors.New("invalid public key: not PEM encoded")
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("invalid public key: %w", err)
		}
		alg := key.Alg
		switch public := parsed.(type) {
		case *rsa.PublicKey:
			if alg == "" {
				alg = a2a.AlgRS256
			}
			out = append(out, a2a.PublicKey{Algorithm: alg, Key: public})
		case *ecdsa.PublicKey:
			if alg == "" {
				alg = a2a.AlgES256
			}
			out = append(out, a2a.PublicKey{Algorithm: alg, Key: public})
		default:
			return nil, fmt.Errorf("unsupported public key type %T", parsed)
		}
	}
	return out, nil
}

func newLiteLifecycle(now func() time.Time) *federation.Lifecycle {
	return federation.NewLifecycle(now)
}
