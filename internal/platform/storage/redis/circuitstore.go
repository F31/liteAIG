package redis

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/F31/liteAIG/internal/resilience/circuit"
	"github.com/redis/go-redis/v9"
)

const circuitFactsKey = "liteaig:circuit:facts"

// CircuitStore persists circuit facts in a Redis hash for restart reconstruction.
type CircuitStore struct {
	client redis.Cmdable
}

func NewCircuitStore(client redis.Cmdable) *CircuitStore {
	return &CircuitStore{client: client}
}

func (s *CircuitStore) Load(ctx context.Context) ([]circuit.Fact, error) {
	values, err := s.client.HGetAll(ctx, circuitFactsKey).Result()
	if err != nil {
		return nil, err
	}
	facts := make([]circuit.Fact, 0, len(values))
	for _, value := range values {
		var fact circuit.Fact
		if err := json.Unmarshal([]byte(value), &fact); err != nil {
			return nil, fmt.Errorf("decode circuit fact: %w", err)
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

func (s *CircuitStore) Save(ctx context.Context, fact circuit.Fact) error {
	encoded, err := json.Marshal(fact)
	if err != nil {
		return err
	}
	field := fact.DeploymentID + ":" + fact.CredentialID
	return s.client.HSet(ctx, circuitFactsKey, field, encoded).Err()
}

func (s *CircuitStore) Delete(ctx context.Context, key circuit.TupleKey) error {
	return s.client.HDel(ctx, circuitFactsKey, key.DeploymentID+":"+key.CredentialID).Err()
}

var _ circuit.Store = (*CircuitStore)(nil)
