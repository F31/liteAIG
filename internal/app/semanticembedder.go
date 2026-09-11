package app

import (
	"context"
	"fmt"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

// semanticEmbedTimeout bounds an embedding call so a slow upstream never
// stalls a cache lookup on the request path.
const semanticEmbedTimeout = 5 * time.Second

// snapshotEmbedder produces embedding vectors by calling the tenant's own
// provider (the same connector path the data plane uses for model calls). It
// resolves a deployment from the snapshot on demand; an unusable deployment
// yields (nil, nil) so the caller silently skips the semantic path (fail open).
type snapshotEmbedder struct {
	resolver *providerResolver
}

func (e *snapshotEmbedder) embedFor(ctx context.Context, snapshot *runtime.TenantRuntimeSnapshot, model, input string) ([]float64, error) {
	deployment, ok := embeddingDeployment(snapshot, model)
	if !ok {
		return nil, nil
	}
	invoker, ok := e.resolver.ResolveCredential(snapshot, deployment, deployment.CredentialID)
	if !ok {
		return nil, nil
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
		return nil, err
	}
	if response == nil || response.Response == nil || len(response.Response.Embeddings) == 0 {
		return nil, fmt.Errorf("upstream returned no embeddings")
	}
	return response.Response.Embeddings[0], nil
}

// boundSemanticEmbedder implements gateway/cache.Embedder for one request's
// snapshot + configured embedding model.
type boundSemanticEmbedder struct {
	inner    *snapshotEmbedder
	snapshot *runtime.TenantRuntimeSnapshot
	model    string
}

func (e *boundSemanticEmbedder) Embed(ctx context.Context, input string, _ int) ([]float64, error) {
	return e.inner.embedFor(ctx, e.snapshot, e.model, input)
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
