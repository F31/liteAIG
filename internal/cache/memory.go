package cache

import (
	"container/list"
	"context"
	"sync"
	"time"
)

// MemoryConfig controls the in-process LRU capacity.
type MemoryConfig struct {
	Capacity int
}

// MemoryStore is an in-memory LRU cache (Lite tier).
type MemoryStore struct {
	mu       sync.Mutex
	capacity int
	entries  map[string]*list.Element
	order    *list.List
	stats    Stats
	clock    func() time.Time
}

type lruEntry struct {
	key   string
	entry *Entry
}

// NewMemoryStore builds an LRU cache with the given capacity.
func NewMemoryStore(config MemoryConfig) *MemoryStore {
	if config.Capacity <= 0 {
		config.Capacity = 1024
	}
	return &MemoryStore{
		capacity: config.Capacity,
		entries:  map[string]*list.Element{},
		order:    list.New(),
		clock:    time.Now,
	}
}

// Get returns a live entry or ErrMiss.
func (s *MemoryStore) Get(_ context.Context, key string) (*Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	element, ok := s.entries[key]
	if !ok {
		s.stats.Misses++
		return nil, ErrMiss
	}
	item := element.Value.(*lruEntry)
	if item.entry.TTL > 0 && s.clock().Sub(item.entry.StoredAt) > item.entry.TTL {
		s.remove(element)
		s.stats.Misses++
		return nil, ErrMiss
	}
	s.order.MoveToFront(element)
	s.stats.Hits++
	return item.entry, nil
}

// Put stores an entry, evicting the least-recently-used item when full.
func (s *MemoryStore) Put(_ context.Context, key string, entry *Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if element, ok := s.entries[key]; ok {
		element.Value.(*lruEntry).entry = entry
		s.order.MoveToFront(element)
		s.stats.Puts++
		return nil
	}
	element := s.order.PushFront(&lruEntry{key: key, entry: entry})
	s.entries[key] = element
	s.stats.Puts++
	for len(s.entries) > s.capacity {
		tail := s.order.Back()
		if tail == nil {
			break
		}
		s.remove(tail)
		s.stats.Evictions++
	}
	return nil
}

// Delete removes an entry.
func (s *MemoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if element, ok := s.entries[key]; ok {
		s.remove(element)
	}
	return nil
}

func (s *MemoryStore) remove(element *list.Element) {
	item := element.Value.(*lruEntry)
	delete(s.entries, item.key)
	s.order.Remove(element)
}

// Stats returns cumulative cache counters.
func (s *MemoryStore) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

var _ Store = (*MemoryStore)(nil)
