package cache

import (
	"context"
	"errors"
	"math"
	"sync"
)

// SemanticStore is a replaceable tenant-namespaced vector index for the
// Semantic Cache. All vectors are keyed by tenant/project/model/dimension/
// version so cross-tenant candidates can never collide or be returned.
type SemanticStore interface {
	Put(context.Context, SemanticKey, []float64) error
	Search(context.Context, SemanticKey, []float64, int) ([]SemanticCandidate, error)
}

// SemanticKey identifies a tenant-namespaced vector collection.
type SemanticKey struct {
	TenantID  string
	ProjectID string
	Model     string
	Dimension int
	Version   int64
	SourceKey string // exact cache key whose response the vector represents
}

// Namespace returns the isolated namespace string for a key.
func (k SemanticKey) Namespace() string {
	return "semantic:" + k.TenantID + ":" + k.ProjectID + ":" + k.Model + ":dim:" + itoa(int64(k.Dimension)) + ":ver:" + itoa(k.Version)
}

// SemanticCandidate is one stored vector whose source key may be served.
type SemanticCandidate struct {
	SourceKey string
	Score     float64
}

// MemorySemanticStore is the default in-memory cosine store (Lite/tests).
type MemorySemanticStore struct {
	mu    sync.Mutex
	index map[string]map[string][]float64 // namespace -> sourceKey -> vector
}

// NewMemorySemanticStore builds an in-memory semantic store.
func NewMemorySemanticStore() *MemorySemanticStore {
	return &MemorySemanticStore{index: map[string]map[string][]float64{}}
}

// Put stores a vector in its tenant namespace.
func (s *MemorySemanticStore) Put(_ context.Context, key SemanticKey, vector []float64) error {
	namespace := key.Namespace()
	s.mu.Lock()
	defer s.mu.Unlock()
	collection := s.index[namespace]
	if collection == nil {
		collection = map[string][]float64{}
		s.index[namespace] = collection
	}
	collection[key.SourceKey] = append([]float64(nil), vector...)
	return nil
}

// Search returns the top-limit candidates above zero in the key's namespace,
// ordered by descending cosine similarity. Cross-tenant vectors are never
// reachable because Search only reads the key's own namespace.
func (s *MemorySemanticStore) Search(_ context.Context, key SemanticKey, query []float64, limit int) ([]SemanticCandidate, error) {
	if limit <= 0 {
		return nil, errors.New("search limit must be positive")
	}
	namespace := key.Namespace()
	s.mu.Lock()
	collection := s.index[namespace]
	if collection == nil {
		s.mu.Unlock()
		return nil, nil
	}
	vectors := make(map[string][]float64, len(collection))
	for sourceKey, vector := range collection {
		vectors[sourceKey] = append([]float64(nil), vector...)
	}
	s.mu.Unlock()

	best := make([]SemanticCandidate, 0, limit)
	for sourceKey, vector := range vectors {
		score := cosine(query, vector)
		if math.IsNaN(score) {
			continue
		}
		best = append(best, SemanticCandidate{SourceKey: sourceKey, Score: score})
	}
	// Stable order by score descending.
	for i := 1; i < len(best); i++ {
		for j := i; j > 0 && best[j].Score > best[j-1].Score; j-- {
			best[j], best[j-1] = best[j-1], best[j]
		}
	}
	if len(best) > limit {
		best = best[:limit]
	}
	return best, nil
}

// cosine returns the cosine similarity of two vectors, or NaN on zero norm.
func cosine(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return math.NaN()
	}
	dot, normA, normB := 0.0, 0.0, 0.0
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return math.NaN()
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
