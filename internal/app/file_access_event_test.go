package app

import (
	"context"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

func TestEmitFileAccessEventRedactsContent(t *testing.T) {
	events := &fileEventSink{}
	p := &litePipeline{events: events, ids: fileEventIDs{}, clock: fileEventClock{now: time.Unix(100, 0)}}
	req := &kernel.RequestContext{
		RequestID:            "request-1",
		SelectedDeploymentID: "deployment-1",
		Interaction:          &interaction.Context{TenantID: "tenant-1", ProjectID: "project-1"},
		Request: &interaction.UnifiedRequest{Kind: interaction.RequestFile, Model: "batch-logical", File: &interaction.FilePayload{
			Operation: "upload",
			Purpose:   "batch",
			Filename:  "input.jsonl",
			Data:      []byte("sensitive request body"),
		}},
		Response: &interaction.UnifiedResponse{File: &interaction.FileResult{ID: "file_upload", Filename: "input.jsonl"}},
	}
	p.emitFileAccessEvent(context.Background(), req)
	if len(events.events) != 1 {
		t.Fatalf("events = %d", len(events.events))
	}
	event := events.events[0]
	if event.Kind != "file.access" || event.ID != "event-1" || event.TenantID != "tenant-1" || event.ProjectID != "project-1" || event.RequestID != "request-1" {
		t.Fatalf("event = %+v", event)
	}
	if event.Attributes["operation"] != "upload" || event.Attributes["file_id"] != "file_upload" || event.Attributes["logical_model"] != "batch-logical" || event.Attributes["deployment_id"] != "deployment-1" || event.Attributes["outcome"] != "success" {
		t.Fatalf("attributes = %+v", event.Attributes)
	}
	if _, ok := event.Attributes["filename"]; ok {
		t.Fatalf("filename leaked: %+v", event.Attributes)
	}
	if _, ok := event.Attributes["content"]; ok {
		t.Fatalf("content leaked: %+v", event.Attributes)
	}
}

type fileEventSink struct{ events []contracts.DomainEvent }

func (s *fileEventSink) Emit(_ context.Context, event contracts.DomainEvent) error {
	s.events = append(s.events, event)
	return nil
}

type fileEventIDs struct{}

func (fileEventIDs) New() (string, error) { return "event-1", nil }

type fileEventClock struct{ now time.Time }

func (c fileEventClock) Now() time.Time { return c.now }
