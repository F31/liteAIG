// Package cache provides the Exact Cache with tenant/project-scoped keys and
// a replaceable store (Memory LRU for Lite, Valkey/Redis for Standard/Enterprise).
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// ErrMiss is returned by stores when a key has no entry.
var ErrMiss = errors.New("cache miss")

// Key identifies one cache entry. Cross-Project sharing is prohibited by construction:
// TenantID and ProjectID are always part of the key.
type Key struct {
	TenantID          string
	ProjectID         string
	LogicalModel      string
	NormalizedRequest string // canonical serialization of semantic request content
	NamespaceVersion  int64  // from the compiled Cache Policy
}

// String returns the scoped, hashed key used by stores.
func (k Key) String() string {
	raw := k.TenantID + "\x00" + k.ProjectID + "\x00" + k.LogicalModel + "\x00" + k.NormalizedRequest + "\x00" + itoa(k.NamespaceVersion)
	sum := sha256.Sum256([]byte(raw))
	return "cache:" + k.TenantID + ":" + k.ProjectID + ":" + hex.EncodeToString(sum[:])
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	negative := v < 0
	if negative {
		v = -v
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	if negative {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}

// Entry is a cached normalized upstream response.
type Entry struct {
	Response json.RawMessage `json:"response"`
	StoredAt time.Time       `json:"stored_at"`
	TTL      time.Duration   `json:"ttl_ms"`
}

// Store is the replaceable cache backend operating on scoped string keys.
type Store interface {
	Get(context.Context, string) (*Entry, error)
	Put(context.Context, string, *Entry) error
	Delete(context.Context, string) error
}

// Stats reports low-cardinality cache telemetry.
type Stats struct {
	Hits          int64
	Misses        int64
	Puts          int64
	Evictions     int64
	SavingsTokens int64
}

// Metrics records cache statistics without depending on an observability implementation.
type Metrics interface {
	Record(Stats)
}
