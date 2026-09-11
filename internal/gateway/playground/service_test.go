package playground

import (
	"context"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"testing"
	"time"
)

type pipeline struct{ request *kernel.RequestContext }

func (p *pipeline) Run(_ context.Context, request *kernel.RequestContext) error {
	p.request = request
	request.Response = &interaction.UnifiedResponse{Choices: []interaction.Choice{{Message: interaction.Message{Content: "ok"}}}}
	request.Usage = &interaction.UnifiedUsage{InputTokens: 2, OutputTokens: 1}
	request.SelectedDeploymentID = "deployment"
	request.Latency = 12 * time.Millisecond
	return nil
}

type ids struct{}

func (ids) New() (string, error) { return "request-1", nil }

type clock struct{}

func (clock) Now() time.Time { return time.Unix(1, 0) }
func TestPlaygroundUsesProductionPipelineContext(t *testing.T) {
	pipeline := &pipeline{}
	service, err := New(pipeline, ids{}, clock{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Run(context.Background(), Input{TenantID: "tenant", ProjectID: "project", Model: "chat", Input: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if pipeline.request.Source != "playground" || pipeline.request.Interaction.Caller.Type != "test_principal" || result.RequestID != "request-1" || result.Output != "ok" || result.Usage.TotalTokens() != 3 {
		t.Fatalf("request=%+v result=%+v", pipeline.request, result)
	}
}
