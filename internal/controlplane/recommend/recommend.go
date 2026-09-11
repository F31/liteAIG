// Package recommend generates suggest-only routing and guardrail optimization
// recommendations. Accepting a recommendation MUST create a Config Draft via a
// DraftCreator; it never mutates production configuration directly.
package recommend

import (
	"context"
	"errors"
	"time"
)

var ErrNoEvidenceWindow = errors.New("recommendation has no evidence window")

// EvidenceWindow bounds the data a recommendation is based on.
type EvidenceWindow struct {
	From, To time.Time
	Samples  int64
	Metric   string
}

// Recommendation is a suggest-only optimization proposal.
type Recommendation struct {
	ID          string
	TenantID    string
	Kind        string // routing|guardrail
	Title       string
	Change      map[string]any
	Evidence    EvidenceWindow
	Explanation string
	CreatedAt   time.Time
	Acceptable  bool
}

// DraftCreator creates a Config Draft from an accepted change without
// activating it (the normal Draft/Publish path).
type DraftCreator interface {
	CreateDraftForRecommendation(context.Context, string, string, map[string]any) (string, error)
}

// Valid returns false when the recommendation lacks an evidence window.
func (r Recommendation) Valid() bool {
	return !r.Evidence.From.IsZero() && !r.Evidence.To.IsZero() && r.Evidence.Samples > 0 && r.Evidence.Metric != ""
}

// Accept creates a Config Draft through the DraftCreator. If the
// recommendation is not backed by an evidence window, it is rejected.
func (r Recommendation) Accept(ctx context.Context, creator DraftCreator, actor string) (draftID string, err error) {
	if !r.Valid() {
		return "", ErrNoEvidenceWindow
	}
	if creator == nil {
		return "", errors.New("no draft creator configured")
	}
	return creator.CreateDraftForRecommendation(ctx, r.TenantID, actor, r.Change)
}

// Service generates recommendations from evidence; it never applies them.
type Service struct {
	now func() time.Time
}

// NewService builds a recommendation generator.
func NewService(now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{now: now}
}

// CostThresholdRecommendation suggests a cost weight shift when observed cost
// exceeds a threshold over the evidence window.
func (s *Service) CostThresholdRecommendation(tenantID, metric string, observed, threshold float64, window EvidenceWindow) *Recommendation {
	if observed <= threshold {
		return nil
	}
	return &Recommendation{
		ID:          tenantID + ":cost:" + window.From.UTC().Format("20060102"),
		TenantID:    tenantID,
		Kind:        "routing",
		Title:       "cost_weight",
		Change:      map[string]any{"score_weights": map[string]float64{"cost": 1.5}},
		Evidence:    window,
		Explanation: "observed cost exceeded threshold",
		CreatedAt:   s.now().UTC(),
		Acceptable:  true,
	}
}
