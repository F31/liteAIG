package app

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/F31/liteAIG/internal/identity"
	"github.com/F31/liteAIG/internal/platform/secrets"
	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/migrations"
)

// TestPepperSurvivesRestart proves virtual API keys stay valid across process
// restarts: the pepper ciphertext persists and the master key comes from the
// 0600 sidecar file.
func TestPepperSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	dsn := "file:" + filepath.Join(t.TempDir(), "pepper.db")

	first, err := bootPepper(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}

	second, err := bootPepper(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	firstSecret, err := first.Resolve(ctx, localPepperRef)
	if err != nil {
		t.Fatal(err)
	}
	secondSecret, err := second.Resolve(ctx, localPepperRef)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstSecret, secondSecret) {
		t.Fatal("key pepper changed across restart; issued virtual keys would stop validating")
	}
	if len(firstSecret) != 32 {
		t.Fatalf("pepper length = %d, want 32", len(firstSecret))
	}
}

func bootPepper(ctx context.Context, dsn string) (*identity.Pepper, error) {
	db, err := sqlite.Open(ctx, dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := migrations.Apply(ctx, db); err != nil {
		return nil, err
	}
	key, err := loadLiteMasterKey(dsn)
	if err != nil {
		return nil, err
	}
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		return nil, err
	}
	store := sqlrepo.Open(db, sqlrepo.Deps{})
	return identity.EnsurePepper(ctx, store.Pepper, cipher, localPepperVersion, localPepperRef)
}
