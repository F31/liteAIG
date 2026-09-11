package coordination

import (
	"testing"
	"time"
)

func TestMemoryBudgetLedgerConformance(t *testing.T) {
	RunBudgetConformance(t, NewMemoryBudgetLedger(nil))
}

func TestMemoryLeaseCoordinatorConformance(t *testing.T) {
	RunLeaseConformance(t, NewMemoryLeaseCoordinator(nil))
}

func TestMemoryLeaseExpiryRecoversCapacity(t *testing.T) {
	now := time.Unix(1, 0)
	clock := func() time.Time { return now }
	coordinator := NewMemoryLeaseCoordinator(clock)

	ok, _, err := coordinator.Acquire(t.Context(), "scope", 1, time.Hour)
	if err != nil || !ok {
		t.Fatalf("acquire = %t %v", ok, err)
	}
	now = now.Add(time.Hour + time.Second)
	second, _, err := coordinator.Acquire(t.Context(), "scope", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !second {
		t.Fatal("expired lease did not release capacity")
	}
}
