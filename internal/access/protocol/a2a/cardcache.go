package a2a

import (
	"sync"
	"time"
)

// cachedCard is one cached discovery result: the parsed card, the server ETag
// used for conditional revalidation, and the caller-visible freshness bound.
type cachedCard struct {
	Card      *AgentCard
	ETag      string
	ExpiresAt time.Time
}

// CardStore is the concurrency-safe cache backing the discovery Client. Keys
// are the normalized base URLs being discovered. Implementations MUST return
// a fresh clone on Get so callers cannot mutate the cached entry.
type CardStore interface {
	Get(key string) (*cachedCard, bool)
	Put(key string, card *AgentCard, etag string, expiresAt time.Time)
}

// MemoryCardStore is an in-memory, concurrency-safe CardStore. Entries expire
// max(ttl after Put, caller ExpiresAt) and are re-checked on every Get, so an
// expired entry is never returned and is garbage-collected lazily.
type MemoryCardStore struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]*memoryEntry
}

type memoryEntry struct {
	card     *cachedCard
	storedAt time.Time
}

// NewMemoryCardStore builds a MemoryCardStore whose entries are stale ttl
// after they are Put, regardless of the caller-supplied ExpiresAt. A non-
// positive ttl disables the age-based bound and leaves only ExpiresAt.
func NewMemoryCardStore(ttl time.Duration) *MemoryCardStore {
	return &MemoryCardStore{ttl: ttl, entries: make(map[string]*memoryEntry)}
}

// Get returns the stored card for key when it is still fresh, re-checking the
// age bound and the caller ExpiresAt on every read. Stale entries are dropped.
func (s *MemoryCardStore) Get(key string) (*cachedCard, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok {
		return nil, false
	}
	if s.expired(entry) {
		delete(s.entries, key)
		return nil, false
	}
	clone := cloneAgentCard(entry.card.Card)
	return &cachedCard{Card: clone, ETag: entry.card.ETag, ExpiresAt: entry.card.ExpiresAt}, true
}

// Put stores (or replaces) the entry for key. expiresAt may be zero to leave
// freshness to the store's own ttl bound.
func (s *MemoryCardStore) Put(key string, card *AgentCard, etag string, expiresAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := &cachedCard{Card: cloneAgentCard(card), ETag: etag, ExpiresAt: expiresAt}
	s.entries[key] = &memoryEntry{card: stored, storedAt: time.Now()}
}

func (s *MemoryCardStore) expired(entry *memoryEntry) bool {
	now := time.Now()
	if s.ttl > 0 && now.Sub(entry.storedAt) >= s.ttl {
		return true
	}
	if !entry.card.ExpiresAt.IsZero() && now.After(entry.card.ExpiresAt) {
		return true
	}
	return false
}

// cloneAgentCard makes a deep-enough copy that mutating the returned card (or
// its signature) cannot corrupt a cached entry.
func cloneAgentCard(card *AgentCard) *AgentCard {
	if card == nil {
		return nil
	}
	clone := *card
	clone.Skills = append([]string(nil), card.Skills...)
	clone.Capabilities = append([]string(nil), card.Capabilities...)
	if card.Signature != nil {
		signature := *card.Signature
		clone.Signature = &signature
	}
	return &clone
}
