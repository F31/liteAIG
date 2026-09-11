package sqlrepo

import (
	"context"
	"fmt"

	"github.com/F31/liteAIG/internal/controlplane/setup"
)

// ResetHalfInitialized rolls back a setup wizard run that committed the initial
// admin/tenant/project but terminated before publishing its first config
// version. Without a published version the tenant is unusable (the Console has
// no config to draft from and the data plane has no snapshot), so the reset
// guards only on a pristine single-tenant bootstrap: exactly one local admin,
// at most the default project, and no published versions. Any residual API
// keys or drafts are setup artifacts that nothing can reference. Returns
// (false, nil) when the state is not a resumable half-setup.
func (r *BootstrapRepository) ResetHalfInitialized(ctx context.Context) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin setup reset: %w", err)
	}
	defer tx.Rollback()

	var admins, tenants, projects, versions int
	if err := tx.QueryRowContext(ctx, `
SELECT
  (SELECT COUNT(*) FROM local_admins),
  (SELECT COUNT(*) FROM tenants),
  (SELECT COUNT(*) FROM projects),
  (SELECT COUNT(*) FROM config_versions)`).
		Scan(&admins, &tenants, &projects, &versions); err != nil {
		return false, fmt.Errorf("inspect setup state: %w", err)
	}
	if admins != 1 || tenants != 1 || projects > 1 || versions != 0 {
		return false, nil
	}

	// The initial admin/tenant/project, any wizard-created key/draft, and
	// orphaned credential secrets (nothing references the latter because no
	// config version exists). Children are deleted before their parents so
	// foreign keys hold.
	for _, statement := range []string{
		`DELETE FROM api_keys`,
		`DELETE FROM config_drafts`,
		`DELETE FROM projects`,
		`DELETE FROM secret_material`,
		`DELETE FROM tenants`,
		`DELETE FROM local_admins`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return false, fmt.Errorf("clear half-initialized setup: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit setup reset: %w", err)
	}
	return true, nil
}

var _ setup.HalfStateResetter = (*BootstrapRepository)(nil)
