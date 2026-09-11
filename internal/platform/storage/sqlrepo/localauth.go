package sqlrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/F31/liteAIG/internal/identity"
)

type LocalCredentialStore struct{ db *sql.DB }

func NewLocalCredentialStore(db *sql.DB) *LocalCredentialStore { return &LocalCredentialStore{db: db} }

func (s *LocalCredentialStore) FindLocalCredential(ctx context.Context, username string) (identity.LocalCredential, error) {
	var value identity.LocalCredential
	err := s.db.QueryRowContext(ctx, `SELECT a.id,
CASE WHEN COALESCE(a.role,'tenant_admin')='system_admin' THEN '' ELSE (SELECT t.id FROM tenants t WHERE t.status='active' ORDER BY t.created_at LIMIT 1) END,
a.username,COALESCE(a.role,'tenant_admin'),a.password_hash,a.status,COALESCE(a.email,'')
FROM local_admins a
WHERE a.username=$1 AND (COALESCE(a.role,'tenant_admin')='system_admin' OR EXISTS (SELECT 1 FROM tenants t WHERE t.status='active'))`, username).Scan(&value.AdminID, &value.TenantID, &value.Username, &value.Role, &value.PasswordHash, &value.Status, &value.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return value, errors.New("credential not found")
	}
	return value, err
}

func (s *LocalCredentialStore) FindLocalUserByID(ctx context.Context, id string) (identity.LocalCredential, error) {
	var value identity.LocalCredential
	err := s.db.QueryRowContext(ctx, `SELECT a.id,
CASE WHEN COALESCE(a.role,'tenant_admin')='system_admin' THEN '' ELSE (SELECT t.id FROM tenants t WHERE t.status='active' ORDER BY t.created_at LIMIT 1) END,
a.username,COALESCE(a.role,'tenant_admin'),a.password_hash,a.status,COALESCE(a.email,'')
FROM local_admins a
WHERE a.id=$1 AND (COALESCE(a.role,'tenant_admin')='system_admin' OR EXISTS (SELECT 1 FROM tenants t WHERE t.status='active'))`, id).Scan(&value.AdminID, &value.TenantID, &value.Username, &value.Role, &value.PasswordHash, &value.Status, &value.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return value, identity.ErrLocalUserNotFound
	}
	return value, err
}

func (s *LocalCredentialStore) ResetLocalPassword(ctx context.Context, username, passwordHash string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE local_admins SET password_hash=$1 WHERE username=$2 AND status='active'`, passwordHash, username)
	if err != nil {
		return fmt.Errorf("reset local password: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return identity.ErrLocalUserNotFound
	}
	return nil
}

func (s *LocalCredentialStore) ListLocalUsers(ctx context.Context) ([]identity.LocalUser, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, COALESCE(role,'tenant_admin'), status, COALESCE(email,''), created_at FROM local_admins ORDER BY created_at, username`)
	if err != nil {
		return nil, fmt.Errorf("list local users: %w", err)
	}
	defer rows.Close()
	var result []identity.LocalUser
	for rows.Next() {
		var user identity.LocalUser
		var createdAt databaseTime
		if err := rows.Scan(&user.ID, &user.Username, &user.Role, &user.Status, &user.Email, &createdAt); err != nil {
			return nil, err
		}
		user.CreatedAt = createdAt.Time
		result = append(result, user)
	}
	return result, rows.Err()
}

func (s *LocalCredentialStore) CreateLocalUser(ctx context.Context, user identity.LocalUser) (identity.LocalUser, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO local_admins(id, username, password_hash, role, status, email) VALUES ($1, $2, $3, $4, 'active', $5)`, user.ID, user.Username, user.PasswordHash, user.Role, user.Email)
	if err != nil {
		if isUniqueViolation(err) {
			return identity.LocalUser{}, identity.ErrUsernameTaken
		}
		return identity.LocalUser{}, fmt.Errorf("create local user: %w", err)
	}
	return s.findUser(ctx, user.ID)
}

func (s *LocalCredentialStore) SetUserRole(ctx context.Context, id, role string) (identity.LocalUser, error) {
	if err := s.executeUpdate(ctx, `UPDATE local_admins SET role=$2 WHERE id=$1`, id, role); err != nil {
		return identity.LocalUser{}, err
	}
	return s.findUser(ctx, id)
}

func (s *LocalCredentialStore) SetUserStatus(ctx context.Context, id, status string) (identity.LocalUser, error) {
	if err := s.executeUpdate(ctx, `UPDATE local_admins SET status=$2 WHERE id=$1`, id, status); err != nil {
		return identity.LocalUser{}, err
	}
	return s.findUser(ctx, id)
}

func (s *LocalCredentialStore) SetUserEmail(ctx context.Context, id, email string) (identity.LocalUser, error) {
	if err := s.executeUpdate(ctx, `UPDATE local_admins SET email=$2 WHERE id=$1`, id, email); err != nil {
		return identity.LocalUser{}, err
	}
	return s.findUser(ctx, id)
}

func (s *LocalCredentialStore) DeleteLocalUser(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM local_admins WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete local user: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return identity.ErrLocalUserNotFound
	}
	return nil
}

func (s *LocalCredentialStore) CountActiveAdmins(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM local_admins WHERE role='tenant_admin' AND status='active'`).Scan(&count)
	return count, err
}

func (s *LocalCredentialStore) executeUpdate(ctx context.Context, query string, args ...any) error {
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update local user: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return identity.ErrLocalUserNotFound
	}
	return nil
}

func (s *LocalCredentialStore) findUser(ctx context.Context, id string) (identity.LocalUser, error) {
	var user identity.LocalUser
	var createdAt databaseTime
	err := s.db.QueryRowContext(ctx, `SELECT id, username, COALESCE(role,'tenant_admin'), status, COALESCE(email,''), created_at FROM local_admins WHERE id=$1`, id).Scan(&user.ID, &user.Username, &user.Role, &user.Status, &user.Email, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return user, identity.ErrLocalUserNotFound
	}
	if err != nil {
		return user, err
	}
	user.CreatedAt = createdAt.Time
	return user, nil
}

var _ identity.LocalCredentialStore = (*LocalCredentialStore)(nil)
