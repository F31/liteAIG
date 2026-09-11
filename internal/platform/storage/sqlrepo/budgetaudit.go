package sqlrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/F31/liteAIG/internal/finops/budget"
)

// BudgetAuditRepository records reservation lifecycle facts for audit and recovery.
// It never participates in admission-time correctness.
type BudgetAuditRepository struct{ db *sql.DB }

func NewBudgetAuditRepository(db *sql.DB) *BudgetAuditRepository {
	return &BudgetAuditRepository{db: db}
}

// Record writes a reservation lifecycle event. Failures are returned so callers can
// alarm, but the caller MUST NOT block the hot path on this write.
func (r *BudgetAuditRepository) Record(ctx context.Context, event budget.AuditEvent) error {
	windowKeys, err := json.Marshal(event.WindowKeys)
	if err != nil {
		return fmt.Errorf("encode window keys: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO budget_reservations(
    id, tenant_id, reservation_id, scope_type, scope_id, estimate, actual, status,
    window_keys, created_at, finalized_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		event.ID, event.TenantID, event.ReservationID, event.ScopeType, nullString(event.ScopeID),
		event.Estimate, event.Actual, event.Status, string(windowKeys), event.CreatedAt, event.FinalizedAt,
	)
	if err != nil {
		return fmt.Errorf("record budget reservation: %w", err)
	}
	return nil
}

var _ budget.AuditRepository = (*BudgetAuditRepository)(nil)
