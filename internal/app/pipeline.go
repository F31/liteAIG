package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/F31/liteAIG/internal/cache"
	"github.com/F31/liteAIG/internal/contextguard"
	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/finops/budget"
	"github.com/F31/liteAIG/internal/finops/pricing"
	gatewayapproval "github.com/F31/liteAIG/internal/gateway/approval"
	gatewaycache "github.com/F31/liteAIG/internal/gateway/cache"
	"github.com/F31/liteAIG/internal/gateway/execution"
	"github.com/F31/liteAIG/internal/gateway/guardrail"
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
	"github.com/F31/liteAIG/internal/observability"
	"github.com/F31/liteAIG/internal/platform/coordination"
	"github.com/F31/liteAIG/internal/platform/egress"
	"github.com/F31/liteAIG/internal/platform/secrets"
	"github.com/F31/liteAIG/internal/policy/rate"
	"github.com/F31/liteAIG/internal/resilience/circuit"
	"github.com/F31/liteAIG/internal/resilience/credentialpool"
	"github.com/F31/liteAIG/internal/resilience/leasegate"
	"github.com/F31/liteAIG/internal/resilience/retry"
	routing "github.com/F31/liteAIG/internal/routing/model"
	"github.com/F31/liteAIG/internal/tokenizer"
)

// litePipeline assembles the fixed seven-stage production pipeline for the
// Lite profile: routing planner + execution executor (mock provider invoker) +
// accounting finalizer, publishing request summaries to the Live bus.
type litePipeline struct {
	tracer             *observability.Tracer
	metricSink         *observability.MetricSink
	planner            *routing.Planner
	executor           *execution.Executor
	store              cache.Store
	semantic           cache.SemanticStore
	embedder           *snapshotEmbedder
	judge              *snapshotJudge
	approvals          gatewayapproval.Handler
	guardrails         *builtin.Engine
	accounting         accounting.Repository
	budget             *budgetEnforcer
	limiter            *rate.Limiter
	ratePolicy         rate.Policy
	tokenizer          *tokenizer.Registry
	tpm                *rate.Meter
	contextGuard       *contextguard.Guard
	contextStrict      bool
	metrics            *routingMetrics
	probeFn            probeFunc
	circuit            *circuit.Breaker
	leases             *leasegate.Gate
	inflight           coordination.InflightCounter
	streams            *StreamRegistry
	live               contracts.TelemetrySink
	events             contracts.EventSink
	ids                contracts.IDGenerator
	clock              contracts.Clock
	settlementCurrency func(ctx context.Context, tenantID string) (string, error)
}

var defaultLiteRatePolicy = rate.Policy{RequestsPerMinute: 600, Burst: 600}

// inflightObserver returns a per-credential inflight reader scoped to one
// deployment for the credential pool's least_inflight / quota_aware selection.
// The scope matches the executor's inflightScope so cross-process counts are
// consistent.
func (p *litePipeline) inflightObserver(tenantID, deploymentID string) func(credentialID string) int {
	return func(credentialID string) int {
		scope := "{tenant:" + tenantID + "}:deployment:" + deploymentID + ":credential:" + credentialID
		value, err := p.inflight.Get(context.Background(), scope)
		if err != nil {
			return 0
		}
		return int(value)
	}
}

// effectiveRatePolicy returns the configured gateway rate policy, falling back
// to the built-in default for pipelines constructed without an explicit one.
func (p *litePipeline) effectiveRatePolicy() rate.Policy {
	if p.ratePolicy.RequestsPerMinute <= 0 {
		return defaultLiteRatePolicy
	}
	return p.ratePolicy
}

// defaultLitePriceVersion is the built-in provider price list (USD per
// million tokens) used to derive ProviderCost when the tenant config carries
// no pricing rules. Cache read/write rates follow the providers' published
// prompt-cache pricing; a zero cache rate falls back to the base input rate.
var defaultLitePriceVersion = pricing.PriceVersion{
	ID: "lite-default-v1",
	Rates: []pricing.Rate{
		{Model: "gpt-4o-mini", Currency: "USD", InputPerMillion: 0.15, OutputPerMillion: 0.60, CacheReadPerMillion: 0.075},
		{Model: "gpt-4o", Currency: "USD", InputPerMillion: 2.50, OutputPerMillion: 10.00, CacheReadPerMillion: 1.25},
		{Model: "gpt-4.1", Currency: "USD", InputPerMillion: 2.00, OutputPerMillion: 8.00, CacheReadPerMillion: 0.50},
		{Model: "o1-mini", Currency: "USD", InputPerMillion: 1.10, OutputPerMillion: 4.40, CacheReadPerMillion: 0.55},
		{Model: "claude-3-5-sonnet", Currency: "USD", InputPerMillion: 3.00, OutputPerMillion: 15.00, CacheReadPerMillion: 0.30, CacheWritePerMillion: 3.75},
		{Model: "claude-3-5-haiku", Currency: "USD", InputPerMillion: 0.80, OutputPerMillion: 4.00, CacheReadPerMillion: 0.08, CacheWritePerMillion: 1.00},
	},
}

// Run implements playground.Pipeline: it builds a per-request runner so the
// routing plan and execution result flow into accounting without shared state.
func (p *litePipeline) Run(ctx context.Context, request *kernel.RequestContext) (runErr error) {
	if request == nil || request.Request == nil || request.Snapshot == nil {
		return errors.New("lite pipeline requires a complete request context")
	}
	if p.tracer != nil {
		var span observability.RequestSpan
		ctx, span = p.tracer.Start(ctx, request, "liteaig.pipeline")
		defer func() { span.End(ctx, runErr) }()
	}
	// Long streams are tracked in the stream registry so drain can wait for
	// them with the longer stream timeout, and no new ones start mid-drain.
	if request.Request.Stream && request.StreamWriter != nil && p.streams != nil {
		streamID, ok := p.streams.Register()
		if !ok {
			return &kernelerrors.Error{Code: "DRAINING", Message: "server is draining", RequestID: request.RequestID, Retryable: true}
		}
		defer p.streams.Unregister(streamID)
	}
	p.circuit.SetConfig(breakerConfigFromSnapshot(request.Snapshot.CircuitConfig()))
	var plan *routing.RoutePlan
	var result *execution.Result
	started := p.clock.Now()

	admission := pipeline.HandlerFunc(func(context.Context, *kernel.RequestContext) (pipeline.Directive, error) {
		return pipeline.Continue, nil
	})
	inputGuardrail := pipeline.HandlerFunc(func(ctx context.Context, req *kernel.RequestContext) (pipeline.Directive, error) {
		return (guardrail.Handler{Engine: p.guardrails, Sink: p.events, IDs: p.ids, Clock: p.clock}).Handle(ctx, req)
	})
	preflight := pipeline.HandlerFunc(func(ctx context.Context, req *kernel.RequestContext) (pipeline.Directive, error) {
		// Approval-gated actions are checked before quota is consumed, so a
		// blocked action neither spends rate/budget nor pollutes the cache.
		if _, err := p.approvals.Handle(ctx, req); err != nil {
			return pipeline.Continue, err
		}
		if decision := p.limiter.Allow(req.TenantID(), req.ProjectID(), p.effectiveRatePolicy()); !decision.Allowed {
			return pipeline.Continue, &kernelerrors.Error{Code: "RATE_LIMITED", Message: "rate limit exceeded", RequestID: req.RequestID, Retryable: true}
		}
		// TPM is pre-reserved on the estimate and reconciled against real
		// usage once the provider responds (spec §14.5).
		estimate := p.estimateTokens(req.Request)
		if decision, _ := p.tpm.Reserve(req.TenantID(), req.ProjectID(), estimate); !decision.Allowed {
			return pipeline.Continue, &kernelerrors.Error{Code: "TPM_LIMITED", Message: "token rate limit exceeded", RequestID: req.RequestID, Retryable: true}
		}
		req.TPMEstimated = estimate
		if err := p.reserveBudget(req); err != nil {
			return pipeline.Continue, err
		}
		// Streaming responses are not cacheable in the Lite profile.
		if req.Request != nil && req.Request.Stream {
			return pipeline.Continue, nil
		}
		directive, err := (gatewaycache.Handler{Store: p.store}).Handle(ctx, req)
		if err != nil || directive != pipeline.Continue {
			return directive, err
		}
		// Exact miss: fall back to similarity lookup when the tenant enabled
		// the semantic cache. A hit skips Resolution/Execution (the handler
		// returns SkipResolutionExecution); misses pre-index the vector and
		// the exact write-back in finalize() completes the entry.
		policy := req.Snapshot.CachePolicy()
		if policy.Semantic.Enabled && p.semantic != nil && p.embedder != nil {
			return (gatewaycache.SemanticHandler{
				Store:     p.semantic,
				Exact:     p.store,
				Embedder:  &boundSemanticEmbedder{inner: p.embedder, snapshot: req.Snapshot, model: policy.Semantic.Model},
				Threshold: policy.Semantic.Threshold,
			}).Handle(ctx, req)
		}
		return directive, nil
	})
	resolution := pipeline.HandlerFunc(func(ctx context.Context, req *kernel.RequestContext) (pipeline.Directive, error) {
		var err error
		plan, err = p.planner.Plan(routing.Input{
			Snapshot:             req.Snapshot,
			Key:                  req.Key,
			LogicalModel:         req.Request.Model,
			ProjectID:            req.ProjectID(),
			RequestID:            req.RequestID,
			RequiredCapabilities: requiredCapabilities(req.Request),
			RequiresTools:        req.Request.Chat != nil && len(req.Request.Chat.Tools) > 0,
			ContextTokens:        estimateContextTokensWith(req.Request, p.tokenizer),
			Circuit:              p.circuit,
			Health:               p.routeHealth(ctx, req.Snapshot),
			Metrics:              p.metrics.snapshot(p.clock.Now()),
		})
		if err != nil {
			return pipeline.Continue, err
		}
		req.SelectedDeploymentID = plan.Selected()
		// Context Guard (spec §12.3): strict mode rejects requests whose
		// estimated input exceeds the selected deployment context window
		// before any provider call or TPM/accounting spend.
		if p.contextGuard != nil && p.contextStrict && plan.Selected() != "" {
			if deployment, ok := req.Snapshot.Deployment(plan.Selected()); ok {
				if _, err := p.contextGuard.Check(req.Request, deployment.UpstreamModel, deployment.ContextWindow); err != nil {
					return pipeline.Continue, &kernelerrors.Error{Code: "CONTEXT_EXCEEDED", Message: "request exceeds the model context window", RequestID: req.RequestID}
				}
			}
		}
		return pipeline.Continue, nil
	})
	executionStage := pipeline.HandlerFunc(func(ctx context.Context, req *kernel.RequestContext) (pipeline.Directive, error) {
		var err error
		if req.Request.Stream && req.StreamWriter != nil {
			streamGuard := p.streamGuardFor(req)
			collector := newStreamCollector(req.StreamWriter, streamGuard)
			attempts, err := p.executor.ExecuteStream(ctx, plan, req.Snapshot, req.Request, collector)
			if err != nil {
				return pipeline.Continue, err
			}
			result = &execution.Result{Attempts: attempts}
			if len(attempts) > 0 {
				req.SelectedDeploymentID = attempts[len(attempts)-1].DeploymentID
			}
			req.Response = collector.Response(p.clock.Now())
			if req.Response != nil {
				req.Usage = &req.Response.Usage
			}
		} else {
			result, err = p.executor.ExecuteWithPool(ctx, plan, req.Snapshot, req.Request, func(deployment runtime.Deployment) (execution.CredentialPicker, error) {
				state := credentialpool.NewSnapshotState(req.Snapshot, deployment.ID)
				if p.circuit != nil {
					state.WithCircuit(p.circuit.Open)
				}
				if p.inflight != nil {
					// Shared (Redis-backed when a coordinator is configured)
					// inflight counts feed the least_inflight / quota_aware
					// pool strategies, so selection prefers the least-busy
					// credential across gateway processes.
					state.WithInflight(p.inflightObserver(req.TenantID(), deployment.ID))
				}
				pool, err := credentialpool.FromSnapshot(req.Snapshot, deployment, state)
				if err != nil {
					return nil, err
				}
				return pool.NewPicker(req.RequestID)
			})
			if err != nil {
				return pipeline.Continue, err
			}
			req.Response = result.Response
			req.SelectedDeploymentID = result.DeploymentID
			if result.Response != nil {
				req.Usage = &result.Response.Usage
			}
		}
		req.Latency = p.clock.Now().Sub(started)
		if req.SelectedDeploymentID != "" && p.metrics != nil {
			p.metrics.begin(req.SelectedDeploymentID)
		}
		return pipeline.Continue, nil
	})
	outputGuardrail := pipeline.HandlerFunc(func(ctx context.Context, req *kernel.RequestContext) (pipeline.Directive, error) {
		handler := guardrail.Handler{Engine: p.guardrails, Output: true, Sink: p.events, IDs: p.ids, Clock: p.clock}
		if policy := req.Snapshot.GuardrailPolicy(); policy.Judge.Enabled || policy.Groundedness.Enabled {
			handler.Groundedness = p.groundednessFor(policy)
			handler.Judge = p.judgeFor(req.Snapshot, policy)
			if action := policy.Judge.Action; action != "" {
				handler.Action = action
			}
		}
		return handler.Handle(ctx, req)
	})
	accountingStage := pipeline.HandlerFunc(func(ctx context.Context, req *kernel.RequestContext) (pipeline.Directive, error) {
		if req.Err != nil {
			if err := p.finalizeFailure(ctx, req, plan, result, req.Err); err != nil {
				return pipeline.Continue, err
			}
			p.reconcileTPM(req)
			p.recordRoutingMetrics(req, false)
			return pipeline.Continue, nil
		}
		if req.Response == nil {
			p.reconcileTPM(req)
			return pipeline.Continue, nil
		}
		if err := p.finalize(ctx, req, plan, result); err != nil {
			return pipeline.Continue, err
		}
		p.emitFileAccessEvent(ctx, req)
		p.reconcileTPM(req)
		p.recordRoutingMetrics(req, true)
		return pipeline.Continue, nil
	})

	runner, err := pipeline.NewRunner(pipeline.Handlers{
		Admission:              admission,
		InputGuardrail:         inputGuardrail,
		PolicyCostPreflight:    preflight,
		Resolution:             resolution,
		ExecutionResilience:    executionStage,
		OutputStreamGuardrail:  outputGuardrail,
		AccountingAndTelemetry: accountingStage,
	})
	if err != nil {
		return err
	}
	return runner.Run(ctx, request)
}

func (p *litePipeline) emitFileAccessEvent(ctx context.Context, req *kernel.RequestContext) {
	if p.events == nil || req == nil || req.Request == nil || req.Request.Kind != interaction.RequestFile || req.Request.File == nil {
		return
	}
	id, err := p.ids.New()
	if err != nil {
		return
	}
	operation := req.Request.File.Operation
	if operation == "" {
		operation = "content"
	}
	fileID := req.Request.File.ID
	if operation == "upload" && req.Response != nil && req.Response.File != nil && req.Response.File.ID != "" {
		fileID = req.Response.File.ID
	}
	_ = p.events.Emit(ctx, contracts.DomainEvent{
		ID:         id,
		Kind:       "file.access",
		OccurredAt: p.clock.Now(),
		TenantID:   req.TenantID(),
		ProjectID:  req.ProjectID(),
		RequestID:  req.RequestID,
		Attributes: map[string]string{
			"operation":     operation,
			"file_id":       fileID,
			"logical_model": req.Request.Model,
			"deployment_id": req.SelectedDeploymentID,
			"outcome":       "success",
		},
	})
}

func requiredCapabilities(request *interaction.UnifiedRequest) []string {
	if request == nil {
		return nil
	}
	if request.Kind == interaction.RequestRerank {
		return []string{"rerank"}
	}
	if request.Kind == interaction.RequestAudio {
		return []string{"audio"}
	}
	if request.Kind == interaction.RequestBatch || request.Kind == interaction.RequestFile {
		return []string{"batch"}
	}
	return nil
}

// groundednessFor builds a deterministic groundedness checker from the
// compiled policy (nil when the checkpoint is disabled).
func (p *litePipeline) groundednessFor(policy runtime.GuardrailPolicy) groundedness.Checker {
	if !policy.Groundedness.Enabled {
		return nil
	}
	return groundedness.NewOverlapChecker(policy.Groundedness.MinOverlap)
}

// judgeFor builds a bound LLM judge from the compiled policy (nil when the
// checkpoint is disabled or no resolver is available).
func (p *litePipeline) judgeFor(snapshot *runtime.TenantRuntimeSnapshot, policy runtime.GuardrailPolicy) judge.Judge {
	if !policy.Judge.Enabled || p.judge == nil {
		return nil
	}
	return &boundJudge{inner: p.judge, snapshot: snapshot, model: policy.Judge.Model}
}

// admitTool applies the request-level admission stages (rate limit + budget
// reservation) to a tool/agent invocation, mirroring the model pipeline's
// PolicyCostPreflight stage so MCP/A2A traffic is governed like model traffic.
func (p *litePipeline) admitTool(request *kernel.RequestContext) error {
	if decision := p.limiter.Allow(request.TenantID(), request.ProjectID(), p.effectiveRatePolicy()); !decision.Allowed {
		return &kernelerrors.Error{Code: "RATE_LIMITED", Message: "rate limit exceeded", RequestID: request.RequestID, Retryable: true}
	}
	return p.reserveBudget(request)
}

// finalizeTool records a tool/agent interaction (success or failure) in
// accounting and the live bus. On failure the reserved budget (if any) is
// released by the finalizer.
// usageFacts carries the per-path differences into the shared usage-recording
// core so the finalize methods cannot drift on accounting fields.
type usageFacts struct {
	logicalModel  string
	deploymentID  string
	outcome       string
	usageSource   string
	inputTokens   int64
	outputTokens  int64
	retries       int
	fallbacks     int
	latency       time.Duration
	agentID       string
	attempts      []accounting.Attempt
	routeEvidence []accounting.RouteEvidence
	// settle records actual tokens against a reservation (success paths only).
	settle bool
	// providerCost, when set, is billed to the tenant in providerCurrency.
	providerCost     *float64
	providerCurrency string
}

// recordUsage builds the immutable accounting facts, runs the finalizer and
// publishes the live event. It is the single place where usage events are
// written, so schema changes touch one function.
func (p *litePipeline) recordUsage(ctx context.Context, request *kernel.RequestContext, facts usageFacts) error {
	usageID, err := p.ids.New()
	if err != nil {
		return err
	}
	now := p.clock.Now()
	value := accounting.Facts{
		UsageEventID: usageID, RequestID: request.RequestID,
		TenantID: request.TenantID(), ProjectID: request.ProjectID(),
		// key_id columns are UUID-typed; the key's internal ID (not the
		// human-facing PublicID hex fragment) is what Postgres accepts.
		KeyID:        request.Key.ID,
		LogicalModel: facts.logicalModel, DeploymentID: facts.deploymentID,
		Outcome: facts.outcome, UsageSource: facts.usageSource,
		InputTokens: facts.inputTokens, OutputTokens: facts.outputTokens,
		RetryCount: facts.retries, FallbackCount: facts.fallbacks,
		LatencyMS:       facts.latency.Milliseconds(),
		SnapshotVersion: request.SnapshotVersion(), SecurityEpoch: request.Snapshot.SecurityEpoch,
		ReceivedAt: request.ReceivedAt, CompletedAt: now,
		AgentID:  facts.agentID,
		Attempts: facts.attempts, RouteEvidence: facts.routeEvidence,
		BudgetPolicyID: request.BudgetPolicyID, ReservationID: request.ReservationID,
	}
	if request.Usage != nil {
		value.CacheReadTokens = request.Usage.CacheReadTokens
		value.CacheWriteTokens = request.Usage.CacheWriteTokens
		value.CachedInputTokens = request.Usage.CachedInputTokens
		value.ReasoningTokens = request.Usage.ReasoningTokens
		value.ToolCalls = request.Usage.ToolCalls
		value.Estimated = request.Usage.Estimated
		value.EstimationMethod = request.Usage.EstimationMethod
	}
	if facts.settle && request.ReservationID != "" {
		actual := facts.inputTokens + facts.outputTokens
		value.ActualTokens = &actual
	}
	if facts.providerCost != nil {
		value.ProviderCost = facts.providerCost
		value.ProviderCurrency = facts.providerCurrency
	}
	finalizer, err := accounting.NewFinalizer(accounting.FinalizerConfig{Timeout: 5 * time.Second}, p.accounting, p.budget, p.events, value)
	if err != nil {
		return err
	}
	if _, err := finalizer.Finalize(ctx); err != nil {
		return err
	}
	if p.live != nil {
		p.live.PublishLive(contracts.LiveEvent{RequestID: request.RequestID, Outcome: value.Outcome, DeploymentID: value.DeploymentID, LatencyMS: value.LatencyMS})
	}
	p.recordOperationalMetrics(value)
	return nil
}

func (p *litePipeline) recordOperationalMetrics(value accounting.Facts) {
	if p.metricSink == nil {
		return
	}
	labels := map[string]string{"source": value.UsageSource, "outcome": value.Outcome}
	_ = p.metricSink.Record(observability.Metric{Name: "liteaig_requests", Value: 1, Labels: labels})
	_ = p.metricSink.Record(observability.Metric{Name: "liteaig_latency_ms", Value: float64(value.LatencyMS), Labels: labels})
	if value.InputTokens > 0 {
		_ = p.metricSink.Record(observability.Metric{Name: "liteaig_tokens", Value: float64(value.InputTokens), Labels: map[string]string{"kind": "input", "source": value.UsageSource}})
	}
	if value.OutputTokens > 0 {
		_ = p.metricSink.Record(observability.Metric{Name: "liteaig_tokens", Value: float64(value.OutputTokens), Labels: map[string]string{"kind": "output", "source": value.UsageSource}})
	}
}

func routeEvidenceFromPlan(plan *routing.RoutePlan) []accounting.RouteEvidence {
	if plan == nil {
		return nil
	}
	var evidence []accounting.RouteEvidence
	for _, item := range plan.Evidence() {
		exclusions := make([]string, 0, len(item.Exclusions))
		for _, exclusion := range item.Exclusions {
			exclusions = append(exclusions, exclusion.Code)
		}
		evidence = append(evidence, accounting.RouteEvidence{DeploymentID: item.DeploymentID, Eligible: item.Eligible, Exclusions: exclusions})
	}
	return evidence
}

func attemptsFromResult(result *execution.Result) ([]accounting.Attempt, int, int) {
	if result == nil {
		return nil, 0, 0
	}
	var attempts []accounting.Attempt
	retries, fallbacks := 0, 0
	for _, attempt := range result.Attempts {
		attempts = append(attempts, accounting.Attempt{DeploymentID: attempt.DeploymentID, Outcome: attempt.Outcome, Number: attempt.Number, Retryable: attempt.Retryable})
		if attempt.Number > 1 {
			retries++
		}
		if attempt.Number == 1 && len(result.Attempts) > 1 {
			fallbacks++
		}
	}
	return attempts, retries, fallbacks
}

func usageSource(source, fallback string) string {
	if source == "" {
		return fallback
	}
	return source
}

func (p *litePipeline) finalizeTool(ctx context.Context, request *kernel.RequestContext, targetID string, response *interaction.UnifiedResponse, cause error) error {
	now := p.clock.Now()
	latency := now.Sub(request.ReceivedAt)
	var inputTokens, outputTokens int64
	if response != nil {
		inputTokens = response.Usage.InputTokens
		outputTokens = response.Usage.OutputTokens
	}
	outcome := "success"
	if cause != nil {
		outcome = outcomeForError(cause)
	}
	source := request.Source
	if request.Interaction != nil && request.Interaction.Protocol != "" {
		source = request.Interaction.Protocol
	}
	if source == "" {
		source = "gateway"
	}
	var agentID string
	if request.Interaction != nil && request.Interaction.Caller.Type == "agent" {
		agentID = request.Interaction.Caller.ID
	}
	return p.recordUsage(ctx, request, usageFacts{
		logicalModel: targetName(request.Request, targetID), deploymentID: targetID,
		outcome: outcome, usageSource: source,
		inputTokens: inputTokens, outputTokens: outputTokens,
		latency: latency, agentID: agentID,
		settle: cause == nil,
	})
}

// reconcileTPM settles the pre-reserved TPM tokens against the real usage so
// the meter reflects what the provider actually consumed (spec §14.5).
func (p *litePipeline) reconcileTPM(request *kernel.RequestContext) {
	if p.tpm == nil || request == nil {
		return
	}
	actual := int64(0)
	if request.Usage != nil {
		actual = request.Usage.TotalTokens()
	}
	p.tpm.Reconcile(request.TenantID(), request.ProjectID(), request.TPMEstimated, actual)
}

func (p *litePipeline) reserveBudget(request *kernel.RequestContext) error {
	if p.budget == nil || request == nil || request.Snapshot == nil || request.Request == nil || request.ReservationID != "" {
		return nil
	}
	policy, ok := budgetPolicyFor(request, p.clock.Now())
	if !ok {
		return nil
	}
	reservationID, err := p.ids.New()
	if err != nil {
		return err
	}
	estimate := p.estimateTokens(request.Request)
	reserved, err := p.budget.Reserve(policy, reservationID, estimate)
	if err != nil {
		if errors.Is(err, budget.ErrExceeded) || errors.Is(err, coordination.ErrSliceOvershoot) {
			return &kernelerrors.Error{Code: "BUDGET_EXCEEDED", Message: "token budget exceeded", RequestID: request.RequestID}
		}
		return err
	}
	request.BudgetPolicyID = reserved.PolicyID
	request.ReservationID = reserved.ID
	request.BudgetEstimated = reserved.EstimatedTokens
	request.BudgetSoftWarning = reserved.Warning
	return nil
}

func budgetPolicyFor(request *kernel.RequestContext, now time.Time) (budget.Policy, bool) {
	if request == nil || request.Snapshot == nil {
		return budget.Policy{}, false
	}
	var selected budget.Policy
	selectedScore := -1
	for _, item := range request.Snapshot.BudgetPolicies() {
		if item.TenantID != "" && item.TenantID != request.TenantID() {
			continue
		}
		if item.ProjectID != "" && item.ProjectID != request.ProjectID() {
			continue
		}
		if item.KeyID != "" && item.KeyID != request.Key.PublicID {
			continue
		}
		duration := time.Duration(item.WindowHours) * time.Hour
		if duration <= 0 {
			continue
		}
		score := 0
		if item.ProjectID != "" {
			score++
		}
		if item.KeyID != "" {
			score += 2
		}
		if score < selectedScore {
			continue
		}
		windowStart := now.Truncate(duration)
		selected = budget.Policy{ID: item.ID, TenantID: item.TenantID, ProjectID: item.ProjectID, KeyID: item.KeyID, Mode: item.Mode, Consistency: item.Consistency, TokenLimit: int64(item.TokenLimit), WindowStart: windowStart, WindowEnd: windowStart.Add(duration)}
		selectedScore = score
	}
	return selected, selectedScore >= 0
}

func (p *litePipeline) estimateTokens(request *interaction.UnifiedRequest) int64 {
	return estimateTokensWith(request, p.tokenizer)
}

func estimateTokens(request *interaction.UnifiedRequest) int64 {
	return estimateTokensWith(request, nil)
}

func estimateTokensWith(request *interaction.UnifiedRequest, registry *tokenizer.Registry) int64 {
	if request == nil {
		return 1
	}
	estimate := int64(1)
	model := request.Model
	if request.Chat != nil {
		var chars int
		for _, msg := range request.Chat.Messages {
			chars += len(msg.Role) + len(msg.Content) + len(msg.ToolCallID)
			for _, call := range msg.ToolCalls {
				chars += len(call.Function.Name) + len(call.Function.Arguments)
			}
		}
		if len(request.Chat.Tools) > 0 {
			if bytes, err := json.Marshal(request.Chat.Tools); err == nil {
				chars += len(bytes)
			}
		}
		if chars > 0 {
			estimate += estimateFor(registry, model, chars)
		}
		if request.Chat.MaxOutputTokens != nil && *request.Chat.MaxOutputTokens > 0 {
			estimate += int64(*request.Chat.MaxOutputTokens)
		}
	}
	if request.Embedding != nil {
		for _, input := range request.Embedding.Inputs {
			estimate += estimateFor(registry, model, len(input))
		}
	}
	if request.Tool != nil {
		estimate += estimateFor(registry, model, len(request.Tool.Name)+len(strings.TrimSpace(string(request.Tool.Arguments))))
	}
	return estimate
}

// estimateFor sizes a rune count to tokens using the registry when available,
// falling back to the legacy chars/4 heuristic so callers outside the pipeline
// keep deterministic behavior.
func estimateFor(registry *tokenizer.Registry, model string, runes int) int64 {
	if registry != nil {
		return registry.EstimateRunes(model, runes).Tokens
	}
	return int64((runes + 3) / 4)
}

func (p *litePipeline) finalize(ctx context.Context, request *kernel.RequestContext, plan *routing.RoutePlan, result *execution.Result) error {
	attempts, retries, fallbacks := attemptsFromResult(result)
	var inputTokens, outputTokens int64
	if request.Usage != nil {
		inputTokens, outputTokens = request.Usage.InputTokens, request.Usage.OutputTokens
	}
	facts := usageFacts{
		logicalModel: request.Request.Model, deploymentID: request.SelectedDeploymentID,
		outcome: "success", usageSource: usageSource(request.Source, "playground"),
		inputTokens: inputTokens, outputTokens: outputTokens,
		retries: retries, fallbacks: fallbacks,
		latency:  request.Latency,
		attempts: attempts, routeEvidence: routeEvidenceFromPlan(plan),
		settle: true,
	}
	if deployment, ok := request.Snapshot.Deployment(request.SelectedDeploymentID); ok {
		cacheRead, cacheWrite := int64(0), int64(0)
		if request.Usage != nil {
			cacheRead, cacheWrite = request.Usage.CacheReadTokens, request.Usage.CacheWriteTokens
		}
		if cost, err := pricing.PriceCached(defaultLitePriceVersion, deployment.UpstreamModel, "USD", inputTokens, outputTokens, cacheRead, cacheWrite); err == nil {
			facts.providerCost = &cost
			facts.providerCurrency = "USD"
			request.ProviderCost = &cost
		}
	}
	if err := p.recordUsage(ctx, request, facts); err != nil {
		return err
	}
	policy := request.Snapshot.CachePolicy()
	if policy.TTLSeconds > 0 {
		if err := gatewaycache.WriteBack(ctx, p.store, request, time.Duration(policy.TTLSeconds)*time.Second); err != nil {
			return err
		}
	}
	return nil
}

// streamGuardFor builds the three-tier streaming guard for a request from the
// tenant guardrail policy, or nil when no streaming guard is configured (no
// guardrail rules to police). Layer 3 (shadow) stays disabled until an external
// provider is wired.
func (p *litePipeline) streamGuardFor(request *kernel.RequestContext) *guardrail.StreamGuard {
	if request == nil || request.Snapshot == nil {
		return nil
	}
	engine, err := guardrail.CompileEngine(request.Snapshot, p.guardrails)
	if err != nil || !engine.HasRules() {
		return nil
	}
	return guardrail.NewStreamGuard(guardrail.StreamGuardConfig{
		Engine:          engine,
		SecurityEvents:  nil,
		Sink:            p.events,
		IDs:             p.ids,
		Clock:           p.clock,
		TenantID:        request.TenantID(),
		ProjectID:       request.ProjectID(),
		RequestID:       request.RequestID,
		SnapshotVersion: request.SnapshotVersion(),
	})
}

// routeHealth builds the per-deployment health map fed to routing. A
// deployment is unhealthy when its circuit is open or its recent rolling
// success rate collapsed; cold deployments with no observations yet fall back
// to an upstream provider probe when one is configured. Unknown deployments
// stay healthy (the circuit and probe remain the authoritative signals).
func (p *litePipeline) routeHealth(ctx context.Context, snapshot *runtime.TenantRuntimeSnapshot) map[string]bool {
	if snapshot == nil {
		return nil
	}
	health := map[string]bool{}
	now := p.clock.Now()
	for _, deployment := range snapshot.Deployments() {
		id := deployment.ID
		healthy := true
		if p.circuit != nil && p.circuit.Open(id, deployment.CredentialID) {
			healthy = false
		}
		if healthy && p.metrics != nil {
			if p.metrics.hasData(id, now) {
				if !p.metrics.healthy(id, now) {
					healthy = false
				}
			} else if p.probeFn != nil && deployment.Status == "enabled" {
				// No recent observations: probe the backing provider once
				// (cached for providerProbeTTL) instead of guessing.
				if provider, ok := snapshot.Provider(deployment.ProviderID); ok {
					credential, credentialOK := snapshot.Credential(deployment.CredentialID)
					if !credentialOK || credential.Status != "enabled" {
						healthy = false
					} else if okProbe, _ := p.probeFn(ctx, provider.Type, provider.Endpoint, credential.SecretRef); !okProbe {
						healthy = false
					}
				}
			}
		}
		health[id] = healthy
	}
	return health
}

// recordRoutingMetrics feeds one completed request into the rolling routing
// window so the soft strategy scores with real latency/cost/success data.
func (p *litePipeline) recordRoutingMetrics(request *kernel.RequestContext, success bool) {
	if p.metrics == nil || request == nil {
		return
	}
	// begin was paired with the request in the execution stage; observe
	// completes it. When no deployment was selected (admission/guardrail
	// failures) there is nothing to record.
	if request.SelectedDeploymentID == "" {
		return
	}
	cost := 0.0
	if request.ProviderCost != nil {
		cost = *request.ProviderCost
	}
	cacheHitRatio := 0.0
	if request.Usage != nil && success {
		// Real cache_affinity source (§11.3): the share of input served from the
		// provider prompt cache, computed from the provider-reported usage.
		input := request.Usage.InputTokens
		if total := input + request.Usage.CacheReadTokens; total > 0 {
			cacheHitRatio = float64(request.Usage.CacheReadTokens) / float64(total)
		}
	}
	p.metrics.observe(request.SelectedDeploymentID, float64(request.Latency.Milliseconds()), cost, cacheHitRatio, success)
}

func (p *litePipeline) finalizeFailure(ctx context.Context, request *kernel.RequestContext, plan *routing.RoutePlan, result *execution.Result, cause error) error {
	attempts, _, _ := attemptsFromResult(result)
	return p.recordUsage(ctx, request, usageFacts{
		logicalModel: request.Request.Model, deploymentID: request.SelectedDeploymentID,
		outcome: outcomeForError(cause), usageSource: usageSource(request.Source, "playground"),
		latency:  request.Latency,
		attempts: attempts, routeEvidence: routeEvidenceFromPlan(plan),
	})
}

func outcomeForError(err error) string {
	var kernelErr *kernelerrors.Error
	if errors.As(err, &kernelErr) {
		return kernelErr.Code
	}
	var blockErr *guardraildomain.BlockError
	if errors.As(err, &blockErr) {
		return "GUARDRAIL_BLOCKED"
	}
	var upstream *contracts.UpstreamError
	if errors.As(err, &upstream) && upstream.Code != "" {
		return upstream.Code
	}
	if errors.Is(err, routing.ErrNoEligibleDeployment) {
		return "NO_ELIGIBLE_DEPLOYMENT"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "TIMEOUT"
	}
	return "ERROR"
}

// breakerConfigFromSnapshot maps a published tenant circuit config onto the
// runtime circuit breaker. An empty config (MinSamples == 0) keeps the built-in
// defaults so a tenant that never set circuit parameters behaves as before.
func breakerConfigFromSnapshot(cc runtime.CircuitConfig) circuit.Config {
	if cc.MinSamples == 0 {
		return circuit.DefaultConfig()
	}
	def := circuit.DefaultConfig()
	cfg := circuit.Config{
		MinSamples:     cc.MinSamples,
		ErrorRate:      cc.ErrorRate,
		Cooldown:       time.Duration(cc.InitialCooldownMS) * time.Millisecond,
		CooldownFactor: cc.CooldownFactor,
		MaxCooldown:    time.Duration(cc.MaxCooldownMS) * time.Millisecond,
	}
	if cfg.ErrorRate <= 0 {
		cfg.ErrorRate = def.ErrorRate
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = def.Cooldown
	}
	if cfg.MaxCooldown <= 0 {
		cfg.MaxCooldown = cfg.Cooldown
	}
	return cfg
}

// estimateContextTokens sizes the input payload for context-window
// eligibility using the tokenizer registry when available, plus the requested
// max output when present.
func estimateContextTokens(request *interaction.UnifiedRequest) int {
	return estimateContextTokensWith(request, nil)
}

func estimateContextTokensWith(request *interaction.UnifiedRequest, registry *tokenizer.Registry) int {
	if request == nil {
		return 0
	}
	var chars int
	switch {
	case request.Chat != nil:
		for _, message := range request.Chat.Messages {
			chars += len(message.Role) + len(message.Content) + len(message.ToolCallID)
			for _, call := range message.ToolCalls {
				chars += len(call.Function.Name) + len(call.Function.Arguments)
			}
		}
		if toolsBytes, err := json.Marshal(request.Chat.Tools); err == nil {
			chars += len(toolsBytes)
		}
		base := int(estimateFor(registry, request.Model, chars))
		if request.Chat.MaxOutputTokens != nil {
			return base + *request.Chat.MaxOutputTokens
		}
		return base
	case request.Embedding != nil:
		for _, input := range request.Embedding.Inputs {
			chars += len(input)
		}
	case request.Rerank != nil:
		chars += len(request.Rerank.Query)
		for _, document := range request.Rerank.Documents {
			chars += len(document)
		}
	case request.Audio != nil:
		chars += len(request.Audio.Data) + len(request.Audio.Prompt) + len(request.Audio.Language)
	case request.Batch != nil:
		chars += len(request.Batch.InputFileID) + len(request.Batch.Endpoint) + len(request.Batch.CompletionWindow)
	case request.File != nil:
		chars += len(request.File.ID)
	case request.Tool != nil:
		chars = len(request.Tool.Arguments) + len(request.Tool.Result)
	}
	return int(estimateFor(registry, request.Model, chars))
}

// newLitePipeline builds the Lite pipeline with a memory cache, a builtin
// guardrail engine, real provider connectors (OpenAI / Anthropic via the
// runtime snapshot), and a bounded retry policy. When coord is non-nil its
// distributed primitives (leases, ledger) are used in place of the in-process
// memory ones, so Standard tier coordinates budget and concurrency over Redis.
func newLitePipeline(accountingRepo accounting.Repository, live contracts.TelemetrySink, ids contracts.IDGenerator, clock contracts.Clock, client *http.Client, secretProvider secrets.Provider, appPolicy egress.Policy, settlementCurrency func(ctx context.Context, tenantID string) (string, error), streams *StreamRegistry, approvals gatewayapproval.Handler, events contracts.EventSink, ratePolicy rate.Policy, gatewayTPM int64, probeFn probeFunc, coord *coordinatorDeps) (*litePipeline, error) {
	engine, err := builtin.New(builtin.Policy{})
	if err != nil {
		return nil, err
	}
	if ratePolicy.RequestsPerMinute <= 0 {
		ratePolicy = defaultLiteRatePolicy
	} else if ratePolicy.Burst <= 0 {
		ratePolicy.Burst = ratePolicy.RequestsPerMinute
	}
	policy := retry.DefaultPolicy()
	policy.BaseBackoff = time.Millisecond
	breaker := circuit.New(circuit.DefaultConfig(), clock)
	var leaseCoordinator coordination.LeaseCoordinator = coordination.NewMemoryLeaseCoordinator(clock.Now)
	if coord != nil && coord.leases != nil {
		leaseCoordinator = coord.leases
	}
	leases := leasegate.New(leaseCoordinator, nil)
	var inflightCounter coordination.InflightCounter
	if coord != nil && coord.inflight != nil {
		inflightCounter = coord.inflight
	}
	resolver := newProviderResolver(client, secretProvider, appPolicy)
	executor := execution.New(policy, resolver, breaker, retry.TimerSleeper{}, func() time.Duration { return 0 }).WithLeases(leases)
	if inflightCounter != nil {
		executor = executor.WithInflight(inflightCounter)
	}
	budgetEnforcer := newBudgetEnforcer(budget.New(clock))
	if coord != nil && coord.ledger != nil {
		budgetEnforcer = budgetEnforcer.withDistributedLedger(coord.ledger, clock, coord.alerts)
	}
	return &litePipeline{
		planner:            &routing.Planner{},
		executor:           executor,
		store:              cache.NewMemoryStore(cache.MemoryConfig{Capacity: 64}),
		semantic:           cache.NewMemorySemanticStore(),
		embedder:           &snapshotEmbedder{resolver: resolver},
		judge:              &snapshotJudge{resolver: resolver},
		approvals:          approvals,
		guardrails:         engine,
		accounting:         accountingRepo,
		budget:             budgetEnforcer,
		limiter:            rate.New(clock),
		ratePolicy:         ratePolicy,
		tokenizer:          tokenizer.New(),
		tpm:                rate.NewMeter(clock, func(_, _ string) int64 { return gatewayTPM }),
		contextGuard:       contextguard.New(tokenizer.New(), true),
		contextStrict:      true,
		metrics:            newRoutingMetrics(clock),
		probeFn:            probeFn,
		circuit:            breaker,
		leases:             leases,
		inflight:           inflightCounter,
		streams:            streams,
		live:               live,
		events:             events,
		ids:                ids,
		clock:              clock,
		settlementCurrency: settlementCurrency,
	}, nil
}
