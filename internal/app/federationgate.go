package app

import (
	"slices"
	"strconv"
	"time"

	"github.com/F31/liteAIG/internal/federation"
	"github.com/F31/liteAIG/internal/kernel"
	kernelerrors "github.com/F31/liteAIG/internal/kernel/errors"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

// maxA2ADelegationHops bounds how many agent-to-agent hops a single task may
// take while crossing federated trust boundaries, preventing unbounded
// delegation loops.
const maxA2ADelegationHops = 3

// maxA2ATaskCalls bounds how many outbound relay calls a single durable A2A
// task may consume (each admitted attempt adds one call). Together with
// maxA2ADelegationHops it caps the relay footprint of one task across retries.
const maxA2ATaskCalls = 16

// a2aTaskRunningTTL is the recovery window for a durable task left in running
// by a crashed relay worker. After this TTL a retry may claim the task again.
const a2aTaskRunningTTL = 5 * time.Minute

// a2aHopsMetadataKey carries the inbound delegation depth on A2A requests.
const a2aHopsMetadataKey = "a2a.delegation.hops"

// a2aHopCount reads the inbound delegation hop count from request metadata
// (0 when absent or not a non-negative integer).
func a2aHopCount(request *interaction.UnifiedRequest) int {
	if request == nil || request.Metadata == nil {
		return 0
	}
	hops, err := strconv.Atoi(request.Metadata[a2aHopsMetadataKey])
	if err != nil || hops < 0 {
		return 0
	}
	return hops
}

// isExternalFederatedAgent reports whether the target agent endpoint is a
// projection of an external federated agent (crosses the trust boundary).
func isExternalFederatedAgent(snapshot *runtime.TenantRuntimeSnapshot, agentID string) bool {
	for _, agent := range snapshot.FederatedAgents() {
		if agent.TrustBoundary != "external_federated" {
			continue
		}
		if agent.ID == agentID || agent.ExternalSubject == agentID {
			return true
		}
	}
	return false
}

// activeVerifiedRelationship finds the active federation relationship for a
// target agent that has at least one verified trust anchor.
func activeVerifiedRelationship(snapshot *runtime.TenantRuntimeSnapshot, agentID string) (runtime.FederationRelationship, bool) {
	for _, relationship := range snapshot.FederationRelationships() {
		if relationship.ExternalAgentID != agentID {
			continue
		}
		if relationship.Status != "active" || !relationship.HasVerifiedAnchor {
			continue
		}
		return relationship, true
	}
	return runtime.FederationRelationship{}, false
}

// projectGranted reports whether the relationship grants the calling project
// access to the external agent. A relationship with no project grants grants
// nothing: relationships must now declare who may call.
func projectGranted(relationship runtime.FederationRelationship, projectID string) bool {
	return len(relationship.ProjectGrants) > 0 && slices.Contains(relationship.ProjectGrants, projectID)
}

// capabilityGranted reports whether the relationship grants the requested
// capability. Empty grants deny every capability.
func capabilityGranted(relationship runtime.FederationRelationship, capability string) bool {
	return len(relationship.CapabilityGrants) > 0 && slices.Contains(relationship.CapabilityGrants, capability)
}

// directionAllowsOutbound reports whether the relationship direction permits an
// outbound A2A call. Empty direction is the legacy/undirected form and stays
// allowed for backward compatibility; only outbound and bidirectional allow
// this gate to pass.
func directionAllowsOutbound(direction string) bool {
	return direction == "" || direction == "outbound" || direction == "bidirectional"
}

// approvedVersionMatches reports whether the relationship's pinned approved
// version matches the endpoint version actually being called. An unset pin (or
// an endpoint that does not declare a version) imposes no constraint.
func approvedVersionMatches(approved, endpointVersion string) bool {
	if approved == "" || endpointVersion == "" {
		return true
	}
	return approved == endpointVersion
}

// dataBoundarySatisfied checks that the relationship's declared data boundary
// is compatible with the calling project's residency policy. Processing region
// lists must intersect when both are declared, and an unexpected boundary
// status fails closed.
func dataBoundarySatisfied(project runtime.Project, relationship runtime.FederationRelationship) bool {
	if relationship.BoundaryStatus != "" && relationship.BoundaryStatus != "unknown" &&
		relationship.BoundaryStatus != "declared" && relationship.BoundaryStatus != "contractually_bound" {
		return false
	}
	if len(relationship.ProcessingRegions) == 0 || len(project.AllowedDataRegions) == 0 {
		return true
	}
	for _, region := range relationship.ProcessingRegions {
		if slices.Contains(project.AllowedDataRegions, region) {
			return true
		}
	}
	return false
}

// enforceA2AFederation gates an outbound A2A call against federated trust: it
// bounds the delegation hop count, requires an active verified relationship for
// external federated targets (fail closed), and applies the local permission
// intersection over the relationship's declared governance facts (project and
// capability grants, direction, data boundary, approved version). On success it
// stamps the interaction with the federation context so downstream stages and
// accounting can see the trust boundary.
func enforceA2AFederation(req *kernel.RequestContext, endpoint runtime.AgentEndpoint, capability string) error {
	if hops := a2aHopCount(req.Request); hops >= maxA2ADelegationHops {
		return &kernelerrors.Error{Code: "FEDERATION_HOP_LIMIT", Message: "delegation hop limit exceeded", RequestID: req.RequestID}
	}
	if !isExternalFederatedAgent(req.Snapshot, endpoint.AgentID) {
		return nil
	}
	relationship, ok := activeVerifiedRelationship(req.Snapshot, endpoint.AgentID)
	if !ok {
		return &kernelerrors.Error{Code: "FEDERATION_UNTRUSTED", Message: "no active verified federation relationship for target agent", RequestID: req.RequestID}
	}
	denied := func(message string) error {
		return &kernelerrors.Error{Code: "FEDERATION_DENIED", Message: message, RequestID: req.RequestID}
	}
	project, projectFound := req.Snapshot.Project(req.Interaction.ProjectID)
	projectActive := projectFound && (project.Status == "active" || project.Status == "")
	if !projectActive {
		return denied("federated call denied: calling project is not active or does not exist")
	}
	if !projectGranted(relationship, req.Interaction.ProjectID) {
		return denied("federated call denied: relationship grants no project grant for calling project " + req.Interaction.ProjectID)
	}
	if !capabilityGranted(relationship, capability) {
		return denied("federated call denied: relationship grants no capability grant for " + capability)
	}
	if !directionAllowsOutbound(relationship.Direction) {
		return denied("federated call denied: relationship direction " + relationship.Direction + " does not allow outbound calls")
	}
	if !approvedVersionMatches(relationship.ApprovedVersion, endpoint.Version) {
		return denied("federated call denied: endpoint version " + endpoint.Version + " does not match approved version " + relationship.ApprovedVersion)
	}
	if !dataBoundarySatisfied(project, relationship) {
		return denied("federated call denied: relationship data boundary does not satisfy calling project residency policy")
	}
	permission := federation.Evaluate(federation.PermissionRequest{
		SystemPolicyEnabled: true,
		TenantActive:        req.Snapshot.Status == "active" || req.Snapshot.Status == "",
		ProjectActive:       projectActive,
		// The caller was already authenticated upstream (API key or inbound
		// federated admission); it is not a lie this gate can re-check.
		CallerAllowed: true,
		// No agent-delegation layer is wired yet, so direct/relay callers hold
		// the local delegation grant unconditionally.
		LocalDelegationGranted: true,
		RelationshipActive:     true,
		ProjectGranted:         projectGranted(relationship, req.Interaction.ProjectID),
		CapabilityGranted:      capabilityGranted(relationship, capability),
		DataBoundarySatisfied:  dataBoundarySatisfied(project, relationship),
	})
	if !permission.Allowed() {
		return denied("federated permission intersection failed")
	}
	req.Interaction.TrustBoundary = interaction.TrustBoundaryExternalFederated
	req.Interaction.Direction = interaction.DirectionOutbound
	req.Interaction.Federation = &interaction.FederationContext{
		RelationshipID:  relationship.ID,
		ExternalAgentID: endpoint.AgentID,
		AssuranceLevel:  relationship.AssuranceLevel,
	}
	return nil
}
