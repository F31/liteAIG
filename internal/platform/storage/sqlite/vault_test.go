package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/F31/liteAIG/internal/platform/secrets"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/migrations"
)

func openTestDB(t *testing.T) (*sql.DB, *sqlrepo.SecretVault) {
	t.Helper()
	db, err := Open(context.Background(), "file:secret-vault?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db, sqlrepo.NewSecretVault(db)
}

func TestSecretVaultRoundTripAndRevocation(t *testing.T) {
	ctx := context.Background()
	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	db, vault := openTestDB(t)
	resolver := sqlrepo.NewSecretResolver(vault, cipher.Decrypt)

	const ref = "local://credential/cred-1"
	plaintext := []byte("sk-live-123")
	ciphertext, err := cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Put(ctx, ref, "tenant-1", ciphertext); err != nil {
		t.Fatal(err)
	}

	got, err := resolver.Resolve(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("resolved %q, want %q", got, plaintext)
	}

	// Rotated material resolves to the new bytes.
	ciphertext, _ = cipher.Encrypt([]byte("sk-live-456"))
	if err := vault.Rotate(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if err := vault.Put(ctx, ref, "tenant-1", ciphertext); err != nil {
		t.Fatal(err)
	}
	got, err = resolver.Resolve(ctx, ref)
	if err != nil || string(got) != "sk-live-456" {
		t.Fatalf("after rotation = %q, %v", got, err)
	}

	// Replace rotates atomically: one statement overwrites the material and
	// leaves the reference enabled (no crash window between Rotate and Put).
	ciphertext, _ = cipher.Encrypt([]byte("sk-live-789"))
	if err := vault.Replace(ctx, ref, "tenant-1", ciphertext); err != nil {
		t.Fatal(err)
	}
	got, err = resolver.Resolve(ctx, ref)
	if err != nil || string(got) != "sk-live-789" {
		t.Fatalf("after replace = %q, %v", got, err)
	}
	var rotatedAt any
	if err := db.QueryRowContext(ctx, `SELECT rotated_at FROM secret_material WHERE secret_ref=$1`, ref).Scan(&rotatedAt); err != nil {
		t.Fatal(err)
	}
	if rotatedAt == nil {
		t.Fatal("Replace did not record rotated_at for the superseded material")
	}

	// Disabled material does not resolve.
	if err := vault.Disable(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(ctx, ref); !errors.Is(err, sqlrepo.ErrSecretNotFound) {
		t.Fatalf("disabled resolve err = %v, want sqlrepo.ErrSecretNotFound", err)
	}

	// Unknown refs are rejected.
	if _, err := resolver.Resolve(ctx, "local://credential/absent"); !errors.Is(err, sqlrepo.ErrSecretNotFound) {
		t.Fatalf("absent resolve err = %v, want sqlrepo.ErrSecretNotFound", err)
	}
	if _, err := resolver.Resolve(ctx, "secret://env/NOPE-MISSING"); err == nil {
		t.Fatal("foreign ref resolved")
	}
}
