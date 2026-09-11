package app

import (
	"context"
	"net/http"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/observability/sink"
)

// analyticsClientTimeout bounds a single analytics insert so a slow or
// unreachable ClickHouse never stalls shutdown drain.
const analyticsClientTimeout = 10 * time.Second

// noopEventSink drops every domain event; it is the default when no analytics
// sink is configured so no event leaves the process.
type noopEventSink struct{}

func (noopEventSink) Emit(context.Context, contracts.DomainEvent) error { return nil }

// newAnalyticsSink builds the data-plane EventSink: a ClickHouse adapter over a
// buffered, backpressured sink when options configure an endpoint, otherwise a
// no-op. It returns the EventSink plus a flush function used by shutdown to
// drain buffered events.
func newAnalyticsSink(options *AnalyticsOptions) (contracts.EventSink, func(context.Context) error, error) {
	if options == nil || options.BaseURL == "" || options.Table == "" {
		return noopEventSink{}, func(context.Context) error { return nil }, nil
	}
	writer, err := sink.NewClickHouseWriter(&http.Client{Timeout: analyticsClientTimeout}, options.BaseURL, options.Table)
	if err != nil {
		return nil, nil, err
	}
	buffered := sink.NewBufferedSink(writer, sink.Config{}, nil)
	return sink.NewAdapter(buffered, nil), buffered.Flush, nil
}
