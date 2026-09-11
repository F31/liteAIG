// Package guardrail adapts deterministic guards to fixed input/output stages.
package guardrail

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	guardraildomain "github.com/F31/liteAIG/internal/guardrail"
	"github.com/F31/liteAIG/internal/guardrail/builtin"
	"github.com/F31/liteAIG/internal/guardrail/groundedness"
	"github.com/F31/liteAIG/internal/guardrail/judge"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	kernelerrors "github.com/F31/liteAIG/internal/kernel/errors"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/pipeline"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/tenancy"
)

type Handler struct {
	Engine         *builtin.Engine
	Output         bool
	Sink           contracts.EventSink
	IDs            contracts.IDGenerator
	Clock          contracts.Clock
	SecurityEvents guardraildomain.SecurityEventStore

	// Optional advanced output checkpoints (Phase 4 groundedness/judge).
	Groundedness groundedness.Checker
	Judge        judge.Judge
	Action       string // block|mark|retroactive
}

// engineCache memoizes compiled engines per (tenant, snapshot version, rule
// fingerprint). Engines are immutable after New, so sharing them across
// requests is safe; this avoids re-compiling regexes on every request.
var engineCache = newEngineCache(32)

type compiledEngineCache struct {
	mu    sync.Mutex
	limit int
	items map[string]*builtin.Engine
}

func newEngineCache(limit int) *compiledEngineCache {
	return &compiledEngineCache{limit: limit, items: map[string]*builtin.Engine{}}
}

func (c *compiledEngineCache) get(key string) (*builtin.Engine, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	engine, ok := c.items[key]
	return engine, ok
}

func (c *compiledEngineCache) put(key string, engine *builtin.Engine) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= c.limit {
		// Publish/rollback events are rare; a whole-cache clear is the
		// cheapest bounded eviction.
		c.items = map[string]*builtin.Engine{}
	}
	c.items[key] = engine
}

func engineCacheKey(snapshot *runtime.TenantRuntimeSnapshot, policy runtime.GuardrailPolicy) string {
	fingerprint := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%d", snapshot.Version, policy.SecurityEpoch, len(policy.Rules))))
	for _, rule := range policy.Rules {
		fingerprint = sha256.Sum256(append(fingerprint[:], rule.ID+rule.Kind+rule.Pattern+rule.Action+rule.Replacement...))
	}
	return snapshot.TenantID + ":" + hex.EncodeToString(fingerprint[:])
}

// engineFor builds the engine from the captured snapshot policy when the
// snapshot has a configured guardrail policy (Mode set), otherwise falls back
// to the fixed engine.
func (h Handler) engineFor(request *kernel.RequestContext) (*builtin.Engine, error) {
	if request != nil && request.Snapshot != nil {
		return CompileEngine(request.Snapshot, h.Engine)
	}
	if h.Engine == nil {
		return nil, errors.New("guardrail handler is not configured")
	}
	return h.Engine, nil
}

// CompileEngine builds the snapshot-configured guardrail engine (cached by
// policy fingerprint), falling back to fallback when the snapshot carries no
// guardrail mode. It is shared by the fixed output checkpoints and the
// streaming three-tier wiring.
func CompileEngine(snapshot *runtime.TenantRuntimeSnapshot, fallback *builtin.Engine) (*builtin.Engine, error) {
	if snapshot != nil {
		policy := snapshot.GuardrailPolicy()
		if policy.Mode != "" {
			key := engineCacheKey(snapshot, policy)
			if cached, ok := engineCache.get(key); ok {
				return cached, nil
			}
			rules := make([]builtin.Rule, 0, len(policy.Rules))
			for _, rule := range policy.Rules {
				rules = append(rules, builtin.Rule{ID: rule.ID, Kind: rule.Kind, Pattern: rule.Pattern, Action: rule.Action, Replacement: rule.Replacement})
			}
			engine, err := builtin.New(builtin.Policy{Version: snapshot.Version, Rules: rules})
			if err != nil {
				return nil, err
			}
			engineCache.put(key, engine)
			return engine, nil
		}
	}
	if fallback == nil {
		return nil, errors.New("guardrail engine is not configured")
	}
	return fallback, nil
}

func (h Handler) Handle(ctx context.Context, request *kernel.RequestContext) (pipeline.Directive, error) {
	if request == nil {
		return pipeline.Continue, errors.New("guardrail handler is not configured")
	}
	engine, err := h.engineFor(request)
	if err != nil {
		return pipeline.Continue, err
	}
	if h.Output {
		if request.Response != nil && request.Response.ToolResult != nil {
			result := engine.Evaluate(request.Response.ToolResult.Content)
			request.Response.ToolResult.Content = result.Content
			request.Response.ToolResult.Provenance = interaction.Provenance{Source: "tool_result", Trusted: false}
			if err := h.handleMatches(ctx, request, result); err != nil {
				return pipeline.Continue, err
			}
		}
		if request.Response == nil {
			return pipeline.Continue, nil
		}
		for i := range request.Response.Choices {
			result := engine.Evaluate(request.Response.Choices[i].Message.Content)
			request.Response.Choices[i].Message.Content = result.Content
			if err := h.handleMatches(ctx, request, result); err != nil {
				return pipeline.Continue, err
			}
			// Tool Call Guard: model-generated arguments are untrusted. A
			// redact match escalates to a block because splicing arguments
			// would break the JSON the client must parse.
			for _, call := range request.Response.Choices[i].Message.ToolCalls {
				if call.Function.Arguments == "" {
					continue
				}
				if result := engine.Evaluate(call.Function.Arguments); len(result.Matches) > 0 {
					if err := h.handleMatches(ctx, request, result); err != nil {
						return pipeline.Continue, err
					}
					return pipeline.Continue, &kernelerrors.Error{Code: "OUTPUT_GUARDRAIL_BLOCKED", Message: "tool call arguments blocked by policy", RequestID: request.RequestID}
				}
			}
		}
		if err := h.checkpointAdvanced(ctx, request); err != nil {
			return pipeline.Continue, err
		}
		return pipeline.Continue, nil
	}
	if request.Request != nil && request.Request.Tool != nil && request.Request.Tool.Result != "" {
		result := engine.Evaluate(request.Request.Tool.Result)
		request.Request.Tool.Result = result.Content
		request.Request.Tool.Provenance = interaction.Provenance{Source: "tool_result", Trusted: false}
		if err := h.handleMatches(ctx, request, result); err != nil {
			return pipeline.Continue, err
		}
	}
	if request.Request != nil && request.Request.File != nil && request.Request.File.Operation == "upload" && len(request.Request.File.Data) > 0 {
		result := engine.Evaluate(string(request.Request.File.Data))
		request.Request.File.Data = []byte(result.Content)
		if err := h.handleMatches(ctx, request, result); err != nil {
			return pipeline.Continue, err
		}
	}
	if request.Request == nil || request.Request.Chat == nil {
		return pipeline.Continue, nil
	}
	for i := range request.Request.Chat.Messages {
		result := engine.Evaluate(request.Request.Chat.Messages[i].Content)
		request.Request.Chat.Messages[i].Content = result.Content
		if err := h.handleMatches(ctx, request, result); err != nil {
			return pipeline.Continue, err
		}
		// Echoed assistant tool calls (model-generated in an earlier turn)
		// are untrusted input; arguments are scanned but never redacted in
		// place, because splicing would break the JSON history.
		for _, call := range request.Request.Chat.Messages[i].ToolCalls {
			if call.Function.Arguments == "" {
				continue
			}
			if result := engine.Evaluate(call.Function.Arguments); len(result.Matches) > 0 {
				if err := h.handleMatches(ctx, request, result); err != nil {
					return pipeline.Continue, err
				}
				return pipeline.Continue, &kernelerrors.Error{Code: "GUARDRAIL_BLOCKED", Message: "tool call arguments blocked by policy", RequestID: request.RequestID}
			}
		}
	}
	return pipeline.Continue, nil
}
func (h Handler) handleMatches(ctx context.Context, request *kernel.RequestContext, result builtin.Result) error {
	for _, match := range result.Matches {
		if h.IDs != nil && h.Clock != nil && (h.Sink != nil || h.SecurityEvents != nil) {
			id, _ := h.IDs.New()
			now := h.Clock.Now()
			if h.Sink != nil {
				_ = h.Sink.Emit(ctx, contracts.DomainEvent{ID: id, Kind: "guardrail.match", OccurredAt: now, TenantID: request.Interaction.TenantID, ProjectID: request.Interaction.ProjectID, RequestID: request.RequestID, Attributes: map[string]string{"rule_id": match.RuleID, "action": match.Action, "content_hash": match.ContentHash}})
			}
			if h.SecurityEvents != nil {
				if err := h.SecurityEvents.Create(ctx, tenancy.TenantScope{TenantID: request.Interaction.TenantID}, guardraildomain.SecurityEvent{ID: id, TenantID: request.Interaction.TenantID, ProjectID: request.Interaction.ProjectID, RequestID: request.RequestID, PolicyID: "compiled", RuleID: match.RuleID, Action: match.Action, ContentHash: match.ContentHash, SnapshotVersion: request.SnapshotVersion(), OccurredAt: now}); err != nil {
					return err
				}
			}
		}
		if result.Blocked {
			return &kernelerrors.Error{Code: "GUARDRAIL_BLOCKED", Message: "content blocked by policy", RequestID: request.RequestID}
		}
	}
	return nil
}

// checkpointAdvanced runs the optional Groundedness and LLM-as-Judge checkpoints
// on the combined response content and applies the configured action. Evidence
// is redacted (content hash + reason), never raw response bodies.
func (h Handler) checkpointAdvanced(ctx context.Context, request *kernel.RequestContext) error {
	if h.Groundedness == nil && h.Judge == nil || request == nil || request.Response == nil {
		return nil
	}
	combined := ""
	for _, choice := range request.Response.Choices {
		combined += choice.Message.Content + "\n"
	}
	contextText := ""
	if request.Request != nil && request.Request.Chat != nil {
		for _, message := range request.Request.Chat.Messages {
			contextText += message.Content + "\n"
		}
	}
	failed := ""
	if h.Groundedness != nil {
		if verdict, err := h.Groundedness.Check(ctx, combined, contextText); err == nil && !verdict.Passed {
			failed = "groundedness:" + verdict.Reason
		}
	}
	if failed == "" && h.Judge != nil {
		if result, err := h.Judge.Verdict(ctx, combined, "output safety/quality"); err == nil && !result.Passed {
			failed = "judge:" + result.Reason
		}
	}
	if failed == "" {
		return nil
	}
	hash := sha256.Sum256([]byte(combined))
	contentHash := hex.EncodeToString(hash[:])
	if h.Action == "block" {
		return &kernelerrors.Error{Code: "OUTPUT_GUARDRAIL_BLOCKED", Message: "output failed advanced guardrail", RequestID: request.RequestID}
	}
	if h.IDs != nil && h.Clock != nil && (h.Sink != nil || h.SecurityEvents != nil) {
		id, _ := h.IDs.New()
		now := h.Clock.Now()
		if h.SecurityEvents != nil {
			_ = h.SecurityEvents.Create(ctx, tenancy.TenantScope{TenantID: request.Interaction.TenantID}, guardraildomain.SecurityEvent{ID: id, TenantID: request.Interaction.TenantID, ProjectID: request.Interaction.ProjectID, RequestID: request.RequestID, PolicyID: "advanced", RuleID: failed, Action: h.Action, ContentHash: contentHash, SnapshotVersion: request.SnapshotVersion(), OccurredAt: now})
		}
	}
	return nil
}

var _ pipeline.Handler = Handler{}
