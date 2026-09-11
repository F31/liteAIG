package circuit

import (
	"context"
	"sync"
)

// MemoryStore is a self-contained in-process Store for Lite and tests.
type MemoryStore struct {
	mu    sync.Mutex
	facts map[TupleKey]Fact
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{facts: make(map[TupleKey]Fact)}
}

func (s *MemoryStore) Load(context.Context) ([]Fact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Fact, 0, len(s.facts))
	for _, fact := range s.facts {
		result = append(result, fact)
	}
	return result, nil
}

func (s *MemoryStore) Save(_ context.Context, fact Fact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.facts[TupleKey{DeploymentID: fact.DeploymentID, CredentialID: fact.CredentialID}] = fact
	return nil
}

func (s *MemoryStore) Delete(_ context.Context, key TupleKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.facts, key)
	return nil
}

var _ Store = (*MemoryStore)(nil)
