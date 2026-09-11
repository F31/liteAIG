package recommend

import (
	"context"
	"errors"
	"testing"
	"time"
)

type draftCreator struct {
	called          bool
	tenantID, actor string
	change          map[string]any
}

func (d *draftCreator) CreateDraftForRecommendation(_ context.Context, tenantID, actor string, change map[string]any) (string, error) {
	d.called = true
	d.tenantID, d.actor, d.change = tenantID, actor, change
	return "draft-9", nil
}

func TestAcceptCreatesDraftAndNeverMutates(t *testing.T) {
	rec := Recommendation{ID: "r", TenantID: "t", Kind: "routing", Change: map[string]any{"agent_endpoint": "cheaper"}, From: time.Unix(1, 0), To: time.Unix(2, 0)}
	creator := &draftCreator{}
	draftID, err := rec.Accept(context.Background(), creator, "admin")
	if err != nil || draftID != "draft-9" {
		t.Fatalf("Accept() = %q, %v", draftID, err)
	}
	if !creator.called || creator.tenantID != "t" || creator.actor != "admin" {
		t.Fatalf("creator = %+v", creator)
	}
}

func TestAcceptRejectedWithoutEvidence(t *testing.T) {
	rec := Recommendation{ID: "r", TenantID: "t", Change: map[string]any{"x": 1}}
	if _, err := rec.Accept(context.Background(), &draftCreator{}, "admin"); !errors.Is(err, ErrNoEvidence) {
		t.Fatalf("Accept() error = %v", err)
	}
}

func TestCheaperEndpointSuggestion(t *testing.T) {
	service := NewService()
	window := [2]time.Time{time.Unix(1, 0), time.Unix(2, 0)}
	if rec := service.CheaperEndpoint("t", "invoice.read", "ep-a", 10, 9, window[0], window[1]); rec != nil {
		t.Fatal("no suggestion expected when cost is comparable")
	}
	rec := service.CheaperEndpoint("t", "invoice.read", "ep-a", 10, 2, window[0], window[1])
	if rec == nil || rec.Title != "cheaper_endpoint:invoice.read" {
		t.Fatalf("rec = %+v", rec)
	}
}
