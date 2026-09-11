// Package recommend generates suggest-only agent-level routing/guardrail
// recommendations. Accepting a recommendation creates a Config Draft; it never
// mutates production.
package recommend

import (
	"context"
	"errors"
	"time"
)

// Recommendation is a suggest-only agent optimization proposal.
type Recommendation struct {
	ID       string
	TenantID string
	Kind     string // routing|guardrail
	Title    string
	Change   map[string]any
	From, To time.Time
	Reason   string
}

// DraftCreator creates a Config Draft from an accepted change.
type DraftCreator interface {
	CreateDraftForRecommendation(context.Context, string, string, map[string]any) (string, error)
}

var ErrNoEvidence = errors.New("agent recommendation has no evidence window")

// Accept creates a Config Draft through the DraftCreator.
func (r Recommendation) Accept(ctx context.Context, creator DraftCreator, actor string) (string, error) {
	if r.From.IsZero() || r.To.IsZero() {
		return "", ErrNoEvidence
	}
	if creator == nil {
		return "", errors.New("no draft creator configured")
	}
	return creator.CreateDraftForRecommendation(ctx, r.TenantID, actor, r.Change)
}

// Service builds agent recommendations from metrics.
type Service struct{}

// NewService builds a recommendation service.
func NewService() *Service { return &Service{} }

// CheaperEndpoint suggests routing to a cheaper endpoint when one costs
// significantly less over the evidence window.
func (s *Service) CheaperEndpoint(tenantID, capability, selected string, selectedCost, cheaperCost float64, from, to time.Time) *Recommendation {
	if cheaperCost >= selectedCost*0.8 {
		return nil
	}
	return &Recommendation{
		ID:       tenantID + ":agent-route:" + from.UTC().Format("20060102"),
		TenantID: tenantID,
		Kind:     "routing",
		Title:    "cheaper_endpoint:" + capability,
		Change:   map[string]any{"agent_endpoint": "cheaper", "capability": capability, "selected": selected},
		From:     from, To: to,
		Reason: "alternative endpoint costs materially less",
	}
}
