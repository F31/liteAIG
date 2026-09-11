package redis

import (
	"context"
	"testing"

	"github.com/F31/liteAIG/internal/resilience/circuit"
)

func TestCircuitStoreRoundTripAndDelete(t *testing.T) {
	client := testClient(t)
	store := NewCircuitStore(client)
	ctx := context.Background()
	if err := client.Del(ctx, circuitFactsKey).Err(); err != nil {
		t.Fatal(err)
	}
	fact := circuit.Fact{DeploymentID: "d", CredentialID: "c", State: "open", OpenedAtMS: 1000, OpenCount: 2}
	if err := store.Save(ctx, fact); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].State != "open" || loaded[0].OpenCount != 2 {
		t.Fatalf("loaded = %+v", loaded)
	}
	if err := store.Delete(ctx, circuit.TupleKey{DeploymentID: "d", CredentialID: "c"}); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 {
		t.Fatalf("facts remain after delete: %+v", loaded)
	}
}
