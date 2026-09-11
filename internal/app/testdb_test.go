package app

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

var testMemoryDatabaseID atomic.Uint64

// testMemoryDSN gives each Lite test fixture its own shared-cache database.
// SQLite's named in-memory databases otherwise leak state while an old fixture
// still has a connection open during cleanup.
func testMemoryDSN(t *testing.T, namespace string) string {
	t.Helper()
	return fmt.Sprintf(
		"file:%s-%s-%d?mode=memory&cache=shared",
		sanitizeTestName(namespace),
		sanitizeTestName(t.Name()),
		testMemoryDatabaseID.Add(1),
	)
}

func sanitizeTestName(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
}
