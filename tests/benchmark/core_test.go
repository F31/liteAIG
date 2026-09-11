package benchmark

import (
	"context"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/pipeline"
	"sort"
	"testing"
	"time"
)

func handlers() pipeline.Handlers {
	handler := pipeline.HandlerFunc(func(context.Context, *kernel.RequestContext) (pipeline.Directive, error) {
		return pipeline.Continue, nil
	})
	return pipeline.Handlers{Admission: handler, InputGuardrail: handler, PolicyCostPreflight: handler, Resolution: handler, ExecutionResilience: handler, OutputStreamGuardrail: handler, AccountingAndTelemetry: handler}
}
func TestCorePipelineP99Budget(t *testing.T) {
	runner, err := pipeline.NewRunner(handlers())
	if err != nil {
		t.Fatal(err)
	}
	const samples = 20000
	durations := make([]time.Duration, samples)
	ctx := context.Background()
	request := &kernel.RequestContext{}
	for i := 0; i < 1000; i++ {
		_ = runner.Run(ctx, request)
	}
	for i := range durations {
		started := time.Now()
		if err := runner.Run(ctx, request); err != nil {
			t.Fatal(err)
		}
		durations[i] = time.Since(started)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p99 := durations[int(float64(samples)*.99)-1]
	t.Logf("core pipeline p99=%s samples=%d", p99, samples)
	if p99 > 6*time.Millisecond {
		t.Fatalf("core pipeline p99 %s exceeds 6ms SLO", p99)
	}
}
func BenchmarkCorePipeline(b *testing.B) {
	runner, _ := pipeline.NewRunner(handlers())
	ctx := context.Background()
	request := &kernel.RequestContext{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = runner.Run(ctx, request)
	}
}
