// Package accounting adapts request finalization to the fixed pipeline stage contract.
package accounting

import (
	"context"
	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/pipeline"
)

type Handler struct{ Finalizer *accounting.Finalizer }

func (h Handler) Handle(ctx context.Context, _ *kernel.RequestContext) (pipeline.Directive, error) {
	_, err := h.Finalizer.Finalize(ctx)
	return pipeline.Continue, err
}

var _ pipeline.Handler = Handler{}
