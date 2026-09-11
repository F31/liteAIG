package sqlrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/F31/liteAIG/internal/controlplane/approval"
	"github.com/F31/liteAIG/internal/tenancy"
)

// ApprovalStore is the SQL persistence for approval requests.
type ApprovalStore struct{ db *sql.DB }

func NewApprovalStore(db *sql.DB) *ApprovalStore { return &ApprovalStore{db: db} }

func (s *ApprovalStore) Create(ctx context.Context, scope tenancy.TenantScope, request approval.Request) error {
	if request.TenantID != scope.TenantID {
		return approval.ErrScopeMismatch
	}
	approvedBy, _ := json.Marshal(request.ApprovedBy)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO approval_requests(id, tenant_id, requester, action, target, dual_approval, status, approved_by, rejected_by, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status, approved_by=EXCLUDED.approved_by,
  rejected_by=EXCLUDED.rejected_by, updated_at=EXCLUDED.updated_at`,
		request.ID, request.TenantID, request.Requester, request.Action, request.Target,
		request.DualApproval, string(request.Status), string(approvedBy), sql.NullString{String: request.RejectedBy, Valid: request.RejectedBy != ""},
		request.CreatedAt, request.UpdatedAt)
	return err
}

func (s *ApprovalStore) Get(ctx context.Context, scope tenancy.TenantScope, id string) (*approval.Request, error) {
	var request approval.Request
	var approvedBy []byte
	var rejectedBy sql.NullString
	var createdAt, updatedAt databaseTime
	err := s.db.QueryRowContext(ctx, `
SELECT id, tenant_id, requester, action, target, dual_approval, status, approved_by, rejected_by, created_at, updated_at
FROM approval_requests WHERE id=$1 AND tenant_id=$2`, id, scope.TenantID).
		Scan(&request.ID, &request.TenantID, &request.Requester, &request.Action, &request.Target,
			&request.DualApproval, (*string)(&request.Status), &approvedBy, &rejectedBy,
			&createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, approval.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	decodeJSON(approvedBy, &request.ApprovedBy)
	request.RejectedBy = rejectedBy.String
	request.CreatedAt, request.UpdatedAt = createdAt.Time, updatedAt.Time
	return &request, nil
}

func (s *ApprovalStore) Update(ctx context.Context, scope tenancy.TenantScope, request approval.Request) error {
	if request.TenantID != scope.TenantID {
		return approval.ErrScopeMismatch
	}
	approvedBy, _ := json.Marshal(request.ApprovedBy)
	res, err := s.db.ExecContext(ctx, `
UPDATE approval_requests SET status=$3, approved_by=$4, rejected_by=$5, updated_at=$6
WHERE id=$1 AND tenant_id=$2`,
		request.ID, request.TenantID, string(request.Status), string(approvedBy),
		sql.NullString{String: request.RejectedBy, Valid: request.RejectedBy != ""}, request.UpdatedAt)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return approval.ErrNotFound
	}
	return nil
}

func (s *ApprovalStore) Find(ctx context.Context, scope tenancy.TenantScope, target, requester string) (*approval.Request, error) {
	var request approval.Request
	var approvedBy []byte
	var rejectedBy sql.NullString
	var createdAt, updatedAt databaseTime
	err := s.db.QueryRowContext(ctx, `
SELECT id, tenant_id, requester, action, target, dual_approval, status, approved_by, rejected_by, created_at, updated_at
FROM approval_requests WHERE tenant_id=$1 AND target=$2 AND requester=$3
ORDER BY created_at DESC, id DESC LIMIT 1`, scope.TenantID, target, requester).
		Scan(&request.ID, &request.TenantID, &request.Requester, &request.Action, &request.Target,
			&request.DualApproval, (*string)(&request.Status), &approvedBy, &rejectedBy,
			&createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, approval.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	decodeJSON(approvedBy, &request.ApprovedBy)
	request.RejectedBy = rejectedBy.String
	request.CreatedAt, request.UpdatedAt = createdAt.Time, updatedAt.Time
	return &request, nil
}

func (s *ApprovalStore) List(ctx context.Context, scope tenancy.TenantScope, limit int) ([]approval.Request, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, tenant_id, requester, action, target, dual_approval, status, approved_by, rejected_by, created_at, updated_at
FROM approval_requests WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT $2`, scope.TenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []approval.Request
	for rows.Next() {
		var request approval.Request
		var approvedBy []byte
		var rejectedBy sql.NullString
		var createdAt, updatedAt databaseTime
		if err := rows.Scan(&request.ID, &request.TenantID, &request.Requester, &request.Action, &request.Target,
			&request.DualApproval, (*string)(&request.Status), &approvedBy, &rejectedBy,
			&createdAt, &updatedAt); err != nil {
			return nil, err
		}
		decodeJSON(approvedBy, &request.ApprovedBy)
		request.RejectedBy = rejectedBy.String
		request.CreatedAt, request.UpdatedAt = createdAt.Time, updatedAt.Time
		result = append(result, request)
	}
	return result, rows.Err()
}

var _ approval.Store = (*ApprovalStore)(nil)
