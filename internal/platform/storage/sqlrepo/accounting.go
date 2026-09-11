package sqlrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/tenancy"
)

type AccountingRepository struct{ db *sql.DB }

func NewAccountingRepository(db *sql.DB) *AccountingRepository { return &AccountingRepository{db: db} }
func (r *AccountingRepository) Finalize(ctx context.Context, facts accounting.Facts) (bool, error) {
	route, _ := json.Marshal(facts.RouteEvidence)
	attempts, _ := json.Marshal(facts.Attempts)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO request_records(request_id,tenant_id,project_id,key_id,logical_model,deployment_id,outcome,input_tokens,output_tokens,retry_count,fallback_count,route_evidence,attempts,guardrail_status,provider_cost,provider_currency,source,latency_ms,tenant_snapshot_version,security_epoch,received_at,completed_at,user_id,org_unit_id,org_path_snapshot,cost_center_id,attribution_trust,session_id,task_id,root_task_id,parent_task_id,agent_id,agent_version,agent_endpoint_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34) ON CONFLICT(request_id) DO NOTHING`, facts.RequestID, facts.TenantID, facts.ProjectID, nullString(facts.KeyID), facts.LogicalModel, nullString(facts.DeploymentID), facts.Outcome, facts.InputTokens, facts.OutputTokens, facts.RetryCount, facts.FallbackCount, string(route), string(attempts), nullString(facts.GuardrailStatus), facts.ProviderCost, nullString(facts.ProviderCurrency), facts.UsageSource, facts.LatencyMS, facts.SnapshotVersion, facts.SecurityEpoch, facts.ReceivedAt, facts.CompletedAt, nullString(facts.UserID), nullString(facts.OrgUnitID), nullString(facts.OrgPathSnapshot), nullString(facts.CostCenterID), attributionTrustOrNone(facts.AttributionTrust), nullString(facts.SessionID), nullString(facts.TaskID), nullString(facts.RootTaskID), nullString(facts.ParentTaskID), nullString(facts.AgentID), nullString(facts.AgentVersion), nullString(facts.AgentEndpointID))
	if err != nil {
		return false, fmt.Errorf("insert request record: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return false, tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_events(id,request_id,tenant_id,project_id,key_id,logical_model,deployment_id,input_tokens,output_tokens,total_tokens,cache_read_tokens,cache_write_tokens,cached_input_tokens,reasoning_tokens,tool_calls,usage_source,estimated,estimation_method,provider_cost,provider_currency,pricing_source,tenant_snapshot_version,security_epoch,status,occurred_at,user_id,org_unit_id,org_path_snapshot,cost_center_id,attribution_trust,session_id,task_id,root_task_id,parent_task_id,agent_id,agent_version,agent_endpoint_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,'manual_phase0',$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36)`, facts.UsageEventID, facts.RequestID, facts.TenantID, facts.ProjectID, nullString(facts.KeyID), facts.LogicalModel, nullString(facts.DeploymentID), facts.InputTokens, facts.OutputTokens, facts.InputTokens+facts.OutputTokens, facts.CacheReadTokens, facts.CacheWriteTokens, facts.CachedInputTokens, facts.ReasoningTokens, facts.ToolCalls, facts.UsageSource, facts.Estimated, nullString(facts.EstimationMethod), facts.ProviderCost, nullString(facts.ProviderCurrency), facts.SnapshotVersion, facts.SecurityEpoch, facts.Outcome, facts.CompletedAt, nullString(facts.UserID), nullString(facts.OrgUnitID), nullString(facts.OrgPathSnapshot), nullString(facts.CostCenterID), attributionTrustOrNone(facts.AttributionTrust), nullString(facts.SessionID), nullString(facts.TaskID), nullString(facts.RootTaskID), nullString(facts.ParentTaskID), nullString(facts.AgentID), nullString(facts.AgentVersion), nullString(facts.AgentEndpointID))
	if err != nil {
		return false, fmt.Errorf("insert usage event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
func (r *AccountingRepository) GetRequest(ctx context.Context, scope tenancy.TenantScope, id string) (*accounting.RequestRecord, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var value accounting.RequestRecord
	var deployment sql.NullString
	var keyID sql.NullString
	var routeJSON, attemptJSON string
	var guardrail, currency, userID, orgUnitID, orgPath, costCenter, sessionID, taskID, rootTaskID, parentTaskID, agentID, agentVersion, agentEndpointID sql.NullString
	var trust string
	var cost sql.NullFloat64
	err := r.db.QueryRowContext(ctx, `SELECT request_id,tenant_id,project_id,key_id,logical_model,deployment_id,outcome,input_tokens,output_tokens,retry_count,fallback_count,route_evidence,attempts,guardrail_status,provider_cost,provider_currency,source,latency_ms,tenant_snapshot_version,security_epoch,user_id,org_unit_id,org_path_snapshot,cost_center_id,attribution_trust,session_id,task_id,root_task_id,parent_task_id,agent_id,agent_version,agent_endpoint_id FROM request_records WHERE tenant_id=$1 AND request_id=$2`, scope.TenantID, id).Scan(&value.RequestID, &value.TenantID, &value.ProjectID, &keyID, &value.LogicalModel, &deployment, &value.Outcome, &value.InputTokens, &value.OutputTokens, &value.RetryCount, &value.FallbackCount, &routeJSON, &attemptJSON, &guardrail, &cost, &currency, &value.Source, &value.LatencyMS, &value.SnapshotVersion, &value.SecurityEpoch, &userID, &orgUnitID, &orgPath, &costCenter, &trust, &sessionID, &taskID, &rootTaskID, &parentTaskID, &agentID, &agentVersion, &agentEndpointID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tenancy.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	value.DeploymentID = deployment.String
	value.KeyID = keyID.String
	value.GuardrailStatus = guardrail.String
	value.ProviderCurrency = currency.String
	value.UserID = userID.String
	value.OrgUnitID = orgUnitID.String
	value.OrgPathSnapshot = orgPath.String
	value.CostCenterID = costCenter.String
	value.AttributionTrust = trust
	value.SessionID, value.TaskID, value.RootTaskID, value.ParentTaskID, value.AgentID = sessionID.String, taskID.String, rootTaskID.String, parentTaskID.String, agentID.String
	value.AgentVersion, value.AgentEndpointID = agentVersion.String, agentEndpointID.String
	if cost.Valid {
		value.ProviderCost = &cost.Float64
	}
	if err := json.Unmarshal([]byte(routeJSON), &value.RouteEvidence); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(attemptJSON), &value.Attempts); err != nil {
		return nil, err
	}
	return &value, nil
}
func (r *AccountingRepository) GetUsage(ctx context.Context, scope tenancy.TenantScope, requestID string) (*accounting.UsageRecord, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var value accounting.UsageRecord
	var user, orgUnit, orgPath, costCenter, trust, sessionID, taskID, rootTaskID, parentTaskID, agentID, agentVersion, agentEndpointID sql.NullString
	var cost sql.NullFloat64
	err := r.db.QueryRowContext(ctx, `SELECT request_id,tenant_id,project_id,logical_model,status,input_tokens,output_tokens,total_tokens,cache_read_tokens,cache_write_tokens,cached_input_tokens,reasoning_tokens,tool_calls,provider_cost,user_id,org_unit_id,org_path_snapshot,cost_center_id,attribution_trust,session_id,task_id,root_task_id,parent_task_id,agent_id,agent_version,agent_endpoint_id FROM usage_events WHERE tenant_id=$1 AND request_id=$2`, scope.TenantID, requestID).Scan(&value.RequestID, &value.TenantID, &value.ProjectID, &value.LogicalModel, &value.Status, &value.InputTokens, &value.OutputTokens, &value.TotalTokens, &value.CacheReadTokens, &value.CacheWriteTokens, &value.CachedInputTokens, &value.ReasoningTokens, &value.ToolCalls, &cost, &user, &orgUnit, &orgPath, &costCenter, &trust, &sessionID, &taskID, &rootTaskID, &parentTaskID, &agentID, &agentVersion, &agentEndpointID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tenancy.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	value.Cost = cost.Float64
	value.UserID, value.OrgUnitID, value.OrgPathSnapshot, value.CostCenterID = user.String, orgUnit.String, orgPath.String, costCenter.String
	value.AttributionTrust = trust.String
	value.SessionID, value.TaskID, value.RootTaskID, value.ParentTaskID, value.AgentID = sessionID.String, taskID.String, rootTaskID.String, parentTaskID.String, agentID.String
	value.AgentVersion, value.AgentEndpointID = agentVersion.String, agentEndpointID.String
	return &value, nil
}
func (r *AccountingRepository) ListRequests(ctx context.Context, scope tenancy.TenantScope, limit int) ([]accounting.RequestRecord, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, errors.New("request history limit must be positive")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT request_id,tenant_id,project_id,key_id,logical_model,deployment_id,outcome,input_tokens,output_tokens,retry_count,fallback_count,guardrail_status,provider_cost,provider_currency,source,latency_ms,tenant_snapshot_version,security_epoch FROM request_records WHERE tenant_id=$1 ORDER BY received_at DESC LIMIT $2`, scope.TenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []accounting.RequestRecord
	for rows.Next() {
		var value accounting.RequestRecord
		var keyID, deployment sql.NullString
		var guardrail, currency sql.NullString
		var cost sql.NullFloat64
		if err := rows.Scan(&value.RequestID, &value.TenantID, &value.ProjectID, &keyID, &value.LogicalModel, &deployment, &value.Outcome, &value.InputTokens, &value.OutputTokens, &value.RetryCount, &value.FallbackCount, &guardrail, &cost, &currency, &value.Source, &value.LatencyMS, &value.SnapshotVersion, &value.SecurityEpoch); err != nil {
			return nil, err
		}
		value.KeyID = keyID.String
		value.DeploymentID = deployment.String
		value.GuardrailStatus = guardrail.String
		value.ProviderCurrency = currency.String
		if cost.Valid {
			value.ProviderCost = &cost.Float64
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

var _ accounting.Repository = (*AccountingRepository)(nil)

// OutcomeMetric aggregates the request ledger for one outcome.
type OutcomeMetric struct {
	Outcome      string
	Requests     int64
	InputTokens  int64
	OutputTokens int64
	LatencyMS    int64
}

// ModelMetric aggregates requests per logical model and outcome.
type ModelMetric struct {
	LogicalModel string
	Outcome      string
	Requests     int64
	LatencyMS    int64
}

// MetricSnapshot is a scrape-time aggregate over the request ledger, exposed
// at /metrics. It is cross-tenant because Lite is single-tenant and the scrape
// is unauthenticated; it carries no prompts or per-request detail.
type MetricSnapshot struct {
	TotalRequests int64
	ByOutcome     []OutcomeMetric
	ByModel       []ModelMetric
}

// Metrics computes the aggregate snapshot for the /metrics endpoint.
func (r *AccountingRepository) Metrics(ctx context.Context) (*MetricSnapshot, error) {
	snapshot := &MetricSnapshot{ByOutcome: []OutcomeMetric{}, ByModel: []ModelMetric{}}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_records`).Scan(&snapshot.TotalRequests); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT outcome, COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(latency_ms),0)
FROM request_records GROUP BY outcome ORDER BY outcome`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var metric OutcomeMetric
		if err := rows.Scan(&metric.Outcome, &metric.Requests, &metric.InputTokens, &metric.OutputTokens, &metric.LatencyMS); err != nil {
			return nil, err
		}
		snapshot.ByOutcome = append(snapshot.ByOutcome, metric)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = r.db.QueryContext(ctx, `
SELECT logical_model, outcome, COUNT(*), COALESCE(SUM(latency_ms),0)
FROM request_records GROUP BY logical_model, outcome ORDER BY logical_model, outcome`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var metric ModelMetric
		if err := rows.Scan(&metric.LogicalModel, &metric.Outcome, &metric.Requests, &metric.LatencyMS); err != nil {
			return nil, err
		}
		snapshot.ByModel = append(snapshot.ByModel, metric)
	}
	return snapshot, rows.Err()
}

func attributionTrustOrNone(trust string) string {
	if trust == "" {
		return "none"
	}
	return trust
}
