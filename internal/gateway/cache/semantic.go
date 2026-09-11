// Package cache adapts the Exact Cache and the Semantic Cache to the fixed
// Stage 3 Policy & Cost Preflight.
package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/F31/liteAIG/internal/cache"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/pipeline"
)

// Embedder turns a normalized request into a vector for semantic search.
type Embedder interface {
	Embed(context.Context, string, int) ([]float64, error)
}

// Threshold is the minimum similarity for a semantic candidate to be served.
const DefaultSemanticThreshold = 0.85

// SemanticHandler is the Stage 3 semantic cache lookup. On a candidate above
// threshold it populates the response and returns SkipResolutionExecution so
// Resolution and Execution are skipped while Output Guardrail and Accounting
// still run (source=semantic_cache).
type SemanticHandler struct {
	Store     cache.SemanticStore
	Exact     cache.Store
	Embedder  Embedder
	Threshold float64
	Metrics   cache.Metrics
	// SecurityBlocked reports whether a request was blocked by a security
	// policy; such requests are never served from or written to the cache.
	SecurityBlocked func(*kernel.RequestContext) bool
}

func (h SemanticHandler) threshold() float64 {
	if h.Threshold > 0 {
		return h.Threshold
	}
	return DefaultSemanticThreshold
}

// Handle performs semantic lookup when the request is cacheable.
func (h SemanticHandler) Handle(ctx context.Context, request *kernel.RequestContext) (pipeline.Directive, error) {
	if h.Store == nil || h.Exact == nil || h.Embedder == nil || request == nil || request.Snapshot == nil || request.Request == nil {
		return pipeline.Continue, nil
	}
	policy := request.Snapshot.CachePolicy()
	if !Cacheable(request.Request, policy) {
		return pipeline.Continue, nil
	}
	if h.SecurityBlocked != nil && h.SecurityBlocked(request) {
		return pipeline.Continue, nil
	}
	key := Key(request, policy)
	raw, _ := json.Marshal(request.Request)
	vector, err := h.Embedder.Embed(ctx, request.Request.Model+"\x00"+string(raw), 0)
	if err != nil || len(vector) == 0 {
		return pipeline.Continue, nil
	}
	searchKey := cache.SemanticKey{
		TenantID: request.TenantID(), ProjectID: request.ProjectID(),
		Model: request.Request.Model, Dimension: len(vector), Version: policy.NamespaceVersion,
	}
	candidates, err := h.Store.Search(ctx, searchKey, vector, 1)
	if err != nil {
		return pipeline.Continue, nil
	}
	if len(candidates) > 0 && candidates[0].Score >= h.threshold() {
		entry, err := h.Exact.Get(ctx, candidates[0].SourceKey)
		if err == nil {
			var response interaction.UnifiedResponse
			if json.Unmarshal(entry.Response, &response) == nil {
				request.Response = &response
				request.CacheHit = true
				request.CacheKey = candidates[0].SourceKey
				request.Source = "semantic_cache"
				if h.Metrics != nil {
					h.Metrics.Record(cache.Stats{Hits: 1, SavingsTokens: response.Usage.TotalTokens()})
				}
				return pipeline.SkipResolutionExecution, nil
			}
		}
	}
	request.CachePending = true
	request.CacheKey = key
	// Index the vector so a later equivalent prompt maps back to this exact
	// cache key; the vector and the response live in the same tenant namespace.
	searchKey.SourceKey = key
	_ = h.Store.Put(ctx, searchKey, vector)
	if h.Metrics != nil {
		h.Metrics.Record(cache.Stats{Misses: 1})
	}
	return pipeline.Continue, nil
}

// WriteBack stores the completed response under the pending exact key (and its
// vector was already indexed). Replay is deterministic and hits run current
// Output Guardrail + Accounting.
func WriteBackSemantic(ctx context.Context, store cache.Store, request *kernel.RequestContext, ttl time.Duration) error {
	return WriteBack(ctx, store, request, ttl)
}

var _ pipeline.Handler = SemanticHandler{}
