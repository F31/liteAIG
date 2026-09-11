package cache

import (
	"context"
	"testing"
)

func TestSemanticCrossTenantIsolation(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySemanticStore()
	tenantA := SemanticKey{TenantID: "t-a", ProjectID: "p", Model: "embed", Dimension: 3, Version: 1, SourceKey: "k-a"}
	tenantB := SemanticKey{TenantID: "t-b", ProjectID: "p", Model: "embed", Dimension: 3, Version: 1, SourceKey: "k-b"}
	query := SemanticKey{TenantID: "t-a", ProjectID: "p", Model: "embed", Dimension: 3, Version: 1, SourceKey: "k-query"}
	if tenantA.Namespace() == tenantB.Namespace() {
		t.Fatal("tenant namespaces must differ")
	}
	if err := store.Put(ctx, tenantA, []float64{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, tenantB, []float64{0, 1, 0}); err != nil {
		t.Fatal(err)
	}
	// Tenant A search must only see tenant A's vector (score 1) and never B's.
	candidates, err := store.Search(ctx, query, []float64{1, 0, 0}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].SourceKey != "k-a" {
		t.Fatalf("cross-tenant candidate leaked: %+v", candidates)
	}
}

func TestSemanticThresholdAndScoring(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySemanticStore()
	key := SemanticKey{TenantID: "t", ProjectID: "p", Model: "embed", Dimension: 2, Version: 1}
	_ = store.Put(ctx, SemanticKey{TenantID: "t", ProjectID: "p", Model: "embed", Dimension: 2, Version: 1, SourceKey: "exact"}, []float64{1, 0})

	// Identical query scores 1.0.
	closeCandidates, err := store.Search(ctx, key, []float64{1, 0}, 1)
	if err != nil || len(closeCandidates) != 1 || closeCandidates[0].Score < 0.99 {
		t.Fatalf("close candidates = %+v, %v", closeCandidates, err)
	}
	// Orthogonal query scores ~0 and should be treated as a miss by the caller.
	farCandidates, err := store.Search(ctx, key, []float64{0, 1}, 1)
	if err != nil || len(farCandidates) != 1 || farCandidates[0].Score > 0.01 {
		t.Fatalf("far candidates = %+v, %v", farCandidates, err)
	}
}

func TestSemanticNamespaceVersionInvalidation(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySemanticStore()
	v1 := SemanticKey{TenantID: "t", ProjectID: "p", Model: "embed", Dimension: 2, Version: 1, SourceKey: "old"}
	v2 := SemanticKey{TenantID: "t", ProjectID: "p", Model: "embed", Dimension: 2, Version: 2, SourceKey: "new"}
	_ = store.Put(ctx, v1, []float64{1, 0})
	_ = store.Put(ctx, v2, []float64{0, 1})
	// A query at version 1 only sees the v1 namespace.
	candidates, err := store.Search(ctx, SemanticKey{TenantID: "t", ProjectID: "p", Model: "embed", Dimension: 2, Version: 1}, []float64{1, 0}, 5)
	if err != nil || len(candidates) != 1 || candidates[0].SourceKey != "old" {
		t.Fatalf("version isolation broken: %+v, %v", candidates, err)
	}
}
