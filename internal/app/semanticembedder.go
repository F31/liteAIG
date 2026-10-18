package app

import (
	"context"
	"fmt"
	"time"

	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

// semanticEmbedTimeout bounds an embedding call so a slow upstream never
// stalls a cache lookup on the request path.
const semanticEmbedTimeout = 5 * time.Second

// auxiliaryCallRecorder records a completed auxiliary provider call against
// the parent request for FinOps and metrics. It is optional so unit tests can
// exercise the embedder/judge without a full pipeline.
type auxiliaryCallRecorder func(context.Context, *kernel.RequestContext, string, string, string, interaction.UnifiedUsage, time.Duration, string, string, int64, error)

// auxiliaryReserver pre-reserves budget for an auxiliary provider call. It
// returns the policy/reservation and the reserved estimate.
type auxiliaryReserver func(*kernel.RequestContext, int64) (string, string, int64, error)

// snapshotEmbedder produces embedding vectors by calling the tenant's own
// provider (the same connector path the data plane uses for model calls). It
// resolves a deployment from the snapshot on demand; an unusable deployment
// yields (nil, nil) so the caller silently skips the semantic path (fail open).
type snapshotEmbedder struct {
	resolver *providerResolver
	record   auxiliaryCallRecorder
	reserve  auxiliaryReserver
}

func (e *snapshotEmbedder) embedFor(ctx context.Context, snapshot *runtime.TenantRuntimeSnapshot, model, input string) ([]float64, string, interaction.UnifiedUsage, error) {
	deployment, ok := embeddingDeployment(snapshot, model)
	if !ok {
		return nil, "", interaction.UnifiedUsage{}, nil
	}
	invoker, ok := e.resolver.ResolveCredential(snapshot, deployment, deployment.CredentialID)
	if !ok {
		return nil, "", interaction.UnifiedUsage{}, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, semanticEmbedTimeout)
	defer cancel()
	response, err := invoker.Invoke(callCtx, contracts.InvocationRequest{
		Target:       contracts.TargetRef{Kind: "model", ID: deployment.ID},
		CredentialID: deployment.CredentialID,
		Request: &interaction.UnifiedRequest{
			Kind:      interaction.RequestEmbedding,
			Model:     model,
			Embedding: &interaction.EmbeddingPayload{Inputs: []string{input}},
		},
	})
	if err != nil {
		return nil, deployment.ID, interaction.UnifiedUsage{}, err
	}
	if response == nil || response.Response == nil || len(response.Response.Embeddings) == 0 {
		return nil, deployment.ID, interaction.UnifiedUsage{}, fmt.Errorf("upstream returned no embeddings")
	}
	return response.Response.Embeddings[0], deployment.ID, response.Response.Usage, nil
}

// boundSemanticEmbedder implements gateway/cache.Embedder for one request's
// snapshot + configured embedding model.
type boundSemanticEmbedder struct {
	inner    *snapshotEmbedder
	snapshot *runtime.TenantRuntimeSnapshot
	request  *kernel.RequestContext
	model    string
}

func (e *boundSemanticEmbedder) Embed(ctx context.Context, input string, _ int) ([]float64, error) {
	var policyID, reservationID string
	var estimated int64
	if e.inner.reserve != nil {
		var reserveErr error
		policyID, reservationID, estimated, reserveErr = e.inner.reserve(e.request, int64((len(input)+3)/4))
		if reserveErr != nil {
			recordAuxiliaryCall("embedding", "budget_blocked", 0)
			return nil, nil
		}
	}
	started := time.Now()
	vector, deploymentID, usage, err := e.inner.embedFor(ctx, e.snapshot, e.model, input)
	if e.inner.record != nil && deploymentID != "" {
		e.inner.record(ctx, e.request, "embedding", e.model, deploymentID, usage, time.Since(started), policyID, reservationID, estimated, err)
	}
	return vector, err
}

// embeddingDeployment picks the deployment to call for embeddings: an enabled
// deployment whose upstream model matches and declares the embedding
// capability, else the first enabled deployment with the same upstream model
// or no capability restriction.
func embeddingDeployment(snapshot *runtime.TenantRuntimeSnapshot, model string) (runtime.Deployment, bool) {
	return capabilityDeployment(snapshot, model, "embedding")
}

// capabilityDeployment picks the deployment for an auxiliary model call
// (embeddings, judge): an enabled deployment whose upstream model matches and
// declares the preferred capability, else the first enabled deployment with
// the same upstream model or no capability restriction.
func capabilityDeployment(snapshot *runtime.TenantRuntimeSnapshot, model, preferred string) (runtime.Deployment, bool) {
	var fallback *runtime.Deployment
	for _, deployment := range snapshot.Deployments() {
		if deployment.Status != "enabled" {
			continue
		}
		if deployment.UpstreamModel == model && hasCapability(deployment.Capabilities, preferred) {
			return deployment, true
		}
		if fallback == nil && deployment.UpstreamModel == model {
			fallback = &deployment
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return runtime.Deployment{}, false
}

func hasCapability(capabilities []string, want string) bool {
	for _, capability := range capabilities {
		if capability == want {
			return true
		}
	}
	return false
}
