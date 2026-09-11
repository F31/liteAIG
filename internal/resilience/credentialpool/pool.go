// Package credentialpool selects and rotates provider credentials for a deployment.
package credentialpool

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"sort"
)

// Strategy selects how a credential is chosen from the eligible set.
type Strategy string

const (
	RoundRobin    Strategy = "round_robin"
	Weighted      Strategy = "weighted"
	LeastInflight Strategy = "least_inflight"
	QuotaAware    Strategy = "quota_aware"
)

var (
	ErrNoEligibleCredential = errors.New("no eligible credential")
	ErrExhausted            = errors.New("credential pool exhausted")
)

// Member is one pool credential with an optional selection weight.
type Member struct {
	CredentialID string
	Weight       int
}

// State is the per-credential dynamic view used for eligibility and selection.
type State interface {
	Eligible(credentialID string) bool
	Inflight(credentialID string) int
	Cost(credentialID string) float64
	Quota(credentialID string) (used, limit float64)
}

// Pool is an immutable membership + strategy with dynamic eligibility.
type Pool struct {
	strategy Strategy
	members  []Member
	state    State
}

// New validates and builds a pool.
func New(strategy Strategy, members []Member, state State) (*Pool, error) {
	if len(members) == 0 {
		return nil, errors.New("credential pool has no members")
	}
	switch strategy {
	case RoundRobin, Weighted, LeastInflight, QuotaAware:
	default:
		return nil, errors.New("unsupported pool strategy " + string(strategy))
	}
	return &Pool{strategy: strategy, members: members, state: state}, nil
}

type candidate struct {
	id     string
	weight int
}

// Order returns eligible credential IDs in selection priority order. The first
// element is the primary choice and the remainder is the rotation order.
func (p *Pool) Order(key string) ([]string, error) {
	eligible := make([]candidate, 0, len(p.members))
	for _, member := range p.members {
		if p.state != nil && !p.state.Eligible(member.CredentialID) {
			continue
		}
		weight := member.Weight
		if weight <= 0 {
			weight = 1
		}
		eligible = append(eligible, candidate{id: member.CredentialID, weight: weight})
	}
	if len(eligible) == 0 {
		return nil, ErrNoEligibleCredential
	}

	switch p.strategy {
	case Weighted:
		scores := make(map[string]float64, len(eligible))
		for _, item := range eligible {
			scores[item.id] = weightedScore(key, item.id, item.weight)
		}
		sort.SliceStable(eligible, func(i, j int) bool { return scores[eligible[i].id] < scores[eligible[j].id] })
	case LeastInflight:
		sort.SliceStable(eligible, func(i, j int) bool {
			left, right := p.state.Inflight(eligible[i].id), p.state.Inflight(eligible[j].id)
			if left == right {
				return eligible[i].id < eligible[j].id
			}
			return left < right
		})
	case QuotaAware:
		sort.SliceStable(eligible, func(i, j int) bool {
			left := quotaRatio(p.state, eligible[i].id)
			right := quotaRatio(p.state, eligible[j].id)
			if left == right {
				return eligible[i].id < eligible[j].id
			}
			return left < right
		})
	default: // round_robin
		start := 0
		if key != "" {
			start = int(hashKey(key) % uint64(len(eligible)))
		}
		ordered := make([]candidate, 0, len(eligible))
		ordered = append(ordered, eligible[start:]...)
		ordered = append(ordered, eligible[:start]...)
		eligible = ordered
	}

	result := make([]string, 0, len(eligible))
	for _, item := range eligible {
		result = append(result, item.id)
	}
	return result, nil
}

// Select returns the primary credential for a routing key.
func (p *Pool) Select(key string) (string, error) {
	order, err := p.Order(key)
	if err != nil {
		return "", err
	}
	return order[0], nil
}

// Picker iterates the rotation order for a single request.
type Picker struct {
	order []string
	index int
}

// NewPicker builds a rotation order for a routing key.
func (p *Pool) NewPicker(key string) (*Picker, error) {
	order, err := p.Order(key)
	if err != nil {
		return nil, err
	}
	return &Picker{order: order}, nil
}

// Next returns the next credential or ErrExhausted.
func (p *Picker) Next() (string, error) {
	if p.index >= len(p.order) {
		return "", ErrExhausted
	}
	id := p.order[p.index]
	p.index++
	return id, nil
}

func quotaRatio(state State, id string) float64 {
	used, limit := state.Quota(id)
	if limit <= 0 {
		return math.MaxFloat64
	}
	return used / limit
}

func hashKey(key string) uint64 {
	sum := sha256.Sum256([]byte(key))
	return binary.BigEndian.Uint64(sum[:8])
}

func weightedScore(key, id string, weight int) float64 {
	sum := sha256.Sum256([]byte(key + "\x00" + id))
	value := binary.BigEndian.Uint64(sum[:8])
	unit := (float64(value) + 1) / (float64(^uint64(0)) + 1)
	return -math.Log(unit) / float64(weight)
}
