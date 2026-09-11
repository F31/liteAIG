package recommend

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRecommendationRequiresEvidenceWindow(t *testing.T) {
	rec := Recommendation{ID: "r", TenantID: "t", Kind: "routing", Change: map[string]any{"x": 1}}
	if rec.Valid() {
		t.Fatal("recommendation without evidence window must be invalid")
	}
	rec.Evidence = EvidenceWindow{From: time.Unix(1, 0), To: time.Unix(2, 0), Samples: 100, Metric: "cost"}
	if !rec.Valid() {
		t.Fatal("recommendation with evidence window must be valid")
	}
}

func TestAcceptCreatesDraftAndNeverMutates(t *testing.T) {
	window := EvidenceWindow{From: time.Unix(1, 0), To: time.Unix(2, 0), Samples: 100, Metric: "cost"}
	rec := Recommendation{ID: "r", TenantID: "t", Kind: "routing", Change: map[string]any{"score_weights": map[string]float64{"cost": 1.5}}, Evidence: window}
	creator := &draftCreator{tenantID: "t", actor: "admin", change: rec.Change}
	draftID, err := rec.Accept(context.Background(), creator, "admin")
	if err != nil || draftID != "draft-1" {
		t.Fatalf("Accept() = %q, %v", draftID, err)
	}
	if !creator.called || creator.tenantID != "t" || creator.actor != "admin" {
		t.Fatalf("creator = %+v", creator)
	}
}

func TestAcceptRejectedWithoutEvidence(t *testing.T) {
	rec := Recommendation{ID: "r", TenantID: "t", Change: map[string]any{"x": 1}}
	if _, err := rec.Accept(context.Background(), &draftCreator{}, "admin"); !errors.Is(err, ErrNoEvidenceWindow) {
		t.Fatalf("Accept() error = %v", err)
	}
}

func TestCostThresholdRecommendation(t *testing.T) {
	service := NewService(func() time.Time { return time.Unix(3, 0) })
	window := EvidenceWindow{From: time.Unix(1, 0), To: time.Unix(2, 0), Samples: 500, Metric: "cost"}
	if rec := service.CostThresholdRecommendation("t", "cost", 100, 200, window); rec != nil {
		t.Fatal("no recommendation expected below threshold")
	}
	rec := service.CostThresholdRecommendation("t", "cost", 300, 200, window)
	if rec == nil || rec.Change["score_weights"] == nil || !rec.Acceptable {
		t.Fatalf("recommendation = %+v", rec)
	}
}

type draftCreator struct {
	called          bool
	tenantID, actor string
	change          map[string]any
}

func (d *draftCreator) CreateDraftForRecommendation(_ context.Context, tenantID, actor string, change map[string]any) (string, error) {
	d.called = true
	d.tenantID, d.actor, d.change = tenantID, actor, change
	return "draft-1", nil
}
