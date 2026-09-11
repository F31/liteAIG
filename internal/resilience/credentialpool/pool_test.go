package credentialpool

import (
	"errors"
	"testing"
)

type staticState struct {
	eligible map[string]bool
	inflight map[string]int
	quota    map[string][2]float64
}

func (s staticState) Eligible(id string) bool {
	return s.eligible == nil || s.eligible[id]
}
func (s staticState) Inflight(id string) int { return s.inflight[id] }
func (s staticState) Cost(string) float64    { return 0 }
func (s staticState) Quota(id string) (float64, float64) {
	if s.quota == nil {
		return 0, 0
	}
	value := s.quota[id]
	return value[0], value[1]
}

func TestEligibilityFiltersPool(t *testing.T) {
	state := staticState{eligible: map[string]bool{"a": true, "b": false, "c": true}}
	pool, err := New(RoundRobin, []Member{{CredentialID: "a"}, {CredentialID: "b"}, {CredentialID: "c"}}, state)
	if err != nil {
		t.Fatal(err)
	}
	order, err := pool.Order("key")
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 {
		t.Fatalf("order = %v", order)
	}
	for _, id := range order {
		if id == "b" {
			t.Fatal("ineligible credential selected")
		}
	}
}

func TestNoEligibleCredential(t *testing.T) {
	pool, err := New(RoundRobin, []Member{{CredentialID: "a"}, {CredentialID: "b"}}, staticState{eligible: map[string]bool{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Select("key"); !errors.Is(err, ErrNoEligibleCredential) {
		t.Fatalf("Select() error = %v", err)
	}
}

func TestWeightedIsDeterministic(t *testing.T) {
	state := staticState{eligible: map[string]bool{"a": true, "b": true, "c": true}}
	pool, err := New(Weighted, []Member{{CredentialID: "a", Weight: 1}, {CredentialID: "b", Weight: 10}, {CredentialID: "c", Weight: 1}}, state)
	if err != nil {
		t.Fatal(err)
	}
	first, err := pool.Order("session-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := pool.Order("session-1")
	if err != nil {
		t.Fatal(err)
	}
	if first[0] != second[0] {
		t.Fatalf("weighted selection not reproducible: %v vs %v", first, second)
	}
}

func TestLeastInflightPrefersLeastLoaded(t *testing.T) {
	state := staticState{
		eligible: map[string]bool{"a": true, "b": true},
		inflight: map[string]int{"a": 5, "b": 1},
	}
	pool, err := New(LeastInflight, []Member{{CredentialID: "a"}, {CredentialID: "b"}}, state)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := pool.Select("key")
	if err != nil {
		t.Fatal(err)
	}
	if selected != "b" {
		t.Fatalf("selected = %s", selected)
	}
}

func TestQuotaAwarePrefersAvailableQuota(t *testing.T) {
	state := staticState{
		eligible: map[string]bool{"a": true, "b": true},
		quota:    map[string][2]float64{"a": {80, 100}, "b": {10, 100}},
	}
	pool, err := New(QuotaAware, []Member{{CredentialID: "a"}, {CredentialID: "b"}}, state)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := pool.Select("key")
	if err != nil {
		t.Fatal(err)
	}
	if selected != "b" {
		t.Fatalf("selected = %s", selected)
	}
}

func TestPickerRotatesUntilExhausted(t *testing.T) {
	state := staticState{eligible: map[string]bool{"a": true, "b": true, "c": true}}
	pool, err := New(RoundRobin, []Member{{CredentialID: "a"}, {CredentialID: "b"}, {CredentialID: "c"}}, state)
	if err != nil {
		t.Fatal(err)
	}
	picker, err := pool.NewPicker("key")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for {
		id, err := picker.Next()
		if errors.Is(err, ErrExhausted) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		seen[id] = true
	}
	if len(seen) != 3 {
		t.Fatalf("rotation visited %v", seen)
	}
}
