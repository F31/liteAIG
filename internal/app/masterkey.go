package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/F31/liteAIG/internal/platform/secrets"
)

// loadLiteMasterKey resolves the envelope master key for the Lite profile.
// File-backed databases persist a 0600 sidecar key next to the database file;
// in-memory databases use an ephemeral key (the data itself is ephemeral).
func loadLiteMasterKey(dsn string) ([]byte, error) {
	path := strings.TrimSpace(dsn)
	switch {
	case strings.HasPrefix(path, "file:"):
		path = strings.TrimPrefix(path, "file:")
		if i := strings.IndexByte(path, '?'); i >= 0 {
			path = path[:i]
		}
		// In-memory databases are ephemeral; an ephemeral key is sufficient.
		if path == "" || path == ":memory:" || strings.Contains(dsn, "mode=memory") {
			return secrets.LoadMasterKey("")
		}
		return secrets.LoadMasterKey(path)
	case strings.HasPrefix(path, "postgres://") || strings.HasPrefix(path, "postgresql://"):
		if os.Getenv("LITEAIG_MASTER_KEY") == "" {
			return nil, fmt.Errorf("postgres storage requires LITEAIG_MASTER_KEY")
		}
		return secrets.LoadMasterKey("")
	default:
		return nil, fmt.Errorf("lite master key requires a file: SQLite DSN or postgres DSN, got %q", dsn)
	}
}
