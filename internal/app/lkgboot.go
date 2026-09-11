package app

import (
	"errors"

	"github.com/F31/liteAIG/internal/platform/lkg"
)

// lkgSchemaVersion is the LKG bundle schema this build writes and accepts.
const lkgSchemaVersion = "v1"

// verifyLKGBundle accepts a persisted LKG bundle only when it is structurally
// complete and internally consistent, so a corrupt or foreign bundle can never
// boot the data plane.
func verifyLKGBundle(bundle *lkg.Bundle) error {
	if bundle == nil {
		return errors.New("nil LKG bundle")
	}
	if bundle.SchemaVersion != lkgSchemaVersion {
		return errors.New("unsupported LKG schema version")
	}
	if bundle.TenantRef == "" || bundle.TenantID == "" || bundle.ConfigVersion == 0 {
		return errors.New("incomplete LKG bundle")
	}
	if bundle.SnapshotData == nil {
		return errors.New("LKG bundle has no snapshot data")
	}
	if bundle.SnapshotData.TenantRef != bundle.TenantRef || bundle.SnapshotData.TenantID != bundle.TenantID {
		return errors.New("LKG bundle tenant mismatch")
	}
	if bundle.SnapshotData.Version != bundle.ConfigVersion {
		return errors.New("LKG bundle version mismatch")
	}
	return nil
}
