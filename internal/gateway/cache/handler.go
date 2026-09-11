// Package cache adapts the Exact Cache to the fixed Stage 3 Policy & Cost
// Preflight and provides cacheability decisions compiled from the snapshot.
package cache

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/F31/liteAIG/internal/cache"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/pipeline"
	"github.com/F31/liteAIG/internal/kernel/runtime"
)

// noCacheHeader is honored to bypass cache read and write.
const noCacheHeader = "no-cache"

// defaultCacheableKinds are the request kinds cached unless the policy overrides.
var defaultCacheableKinds = map[string]bool{string(interaction.RequestChat): true, string(interaction.RequestEmbedding): true}

// Cacheable reports whether a request may be cached under the policy.
func Cacheable(request *interaction.UnifiedRequest, policy runtime.CachePolicy) bool {
	if request == nil || !policy.Enabled {
		return false
	}
	if request.Metadata != nil && strings.EqualFold(request.Metadata["cache-control"], noCacheHeader) {
		return false
	}
	allowed := defaultCacheableKinds
	if len(policy.AllowedKinds) > 0 {
		allowed = map[string]bool{}
		for _, kind := range policy.AllowedKinds {
			allowed[kind] = true
		}
	}
	if !allowed[string(request.Kind)] {
		return false
	}
	// Tool calls are never cached by default.
	if request.ToolCalls() {
		return false
	}
	// Multimodal (image-bearing) requests are never cached: the exact key would
	// have to embed image bytes and semantic caching cannot derive meaning from
	// a URI. The responses kind is also excluded via defaultCacheableKinds.
	if request.HasImages() {
		return false
	}
	if policy.MaxTemperature > 0 {
		temp := 0.0
		if request.Chat != nil && request.Chat.Temperature != nil {
			temp = *request.Chat.Temperature
		}
		if temp > policy.MaxTemperature {
			return false
		}
	}
	return true
}

// Key builds the scoped cache key string for a request.
func Key(request *kernel.RequestContext, policy runtime.CachePolicy) string {
	normalized, _ := json.Marshal(request.Request)
	return cache.Key{
		TenantID:          request.TenantID(),
		ProjectID:         request.ProjectID(),
		LogicalModel:      request.Request.Model,
		NormalizedRequest: string(normalized),
		NamespaceVersion:  policy.NamespaceVersion,
	}.String()
}

// Handler implements the Stage 3 cache lookup; on a hit it populates the
// response and returns SkipResolutionExecution so Resolution and Execution are
// skipped while Output Guardrail and Accounting still run.
type Handler struct {
	Store   cache.Store
	Metrics cache.Metrics
}

// Handle looks up the Exact Cache according to the snapshot Cache Policy.
func (h Handler) Handle(ctx context.Context, request *kernel.RequestContext) (pipeline.Directive, error) {
	if h.Store == nil || request == nil || request.Snapshot == nil || request.Request == nil {
		return pipeline.Continue, nil
	}
	policy := request.Snapshot.CachePolicy()
	if !Cacheable(request.Request, policy) {
		return pipeline.Continue, nil
	}
	key := Key(request, policy)
	entry, err := h.Store.Get(ctx, key)
	if err == nil {
		var response interaction.UnifiedResponse
		if json.Unmarshal(entry.Response, &response) == nil {
			request.Response = &response
			request.CacheHit = true
			request.CacheKey = key
			request.Source = "cache"
			if h.Metrics != nil {
				h.Metrics.Record(cache.Stats{Hits: 1, SavingsTokens: response.Usage.TotalTokens()})
			}
			return pipeline.SkipResolutionExecution, nil
		}
		_ = h.Store.Delete(ctx, key)
	}
	if h.Metrics != nil {
		h.Metrics.Record(cache.Stats{Misses: 1})
	}
	request.CachePending = true
	request.CacheKey = key
	return pipeline.Continue, nil
}

// WriteBack stores the completed response under the pending cache key. It is
// invoked after Accounting so that cached output re-runs the current Output
// Guardrail and Accounting on every hit.
func WriteBack(ctx context.Context, store cache.Store, request *kernel.RequestContext, ttl time.Duration) error {
	if store == nil || request == nil || !request.CachePending || request.Response == nil || request.CacheKey == "" {
		return nil
	}
	raw, err := json.Marshal(request.Response)
	if err != nil {
		return err
	}
	return store.Put(ctx, request.CacheKey, &cache.Entry{Response: raw, StoredAt: time.Now(), TTL: ttl})
}

var _ pipeline.Handler = Handler{}
