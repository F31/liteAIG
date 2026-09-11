package app

import (
	"context"
	"strings"
	"sync"
	"time"

	gatewayguardrail "github.com/F31/liteAIG/internal/gateway/guardrail"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
)

// streamCollector tees normalized stream events to the live writer while
// assembling the unified response used by the output guardrail and accounting
// stages after the stream completes. When a three-tier StreamGuard is
// attached, text deltas run through Inline/Buffered/Shadow checks (spec §16)
// before they reach the writer; a block aborts the stream.
type streamCollector struct {
	out contracts.StreamWriter
	gr  *gatewayguardrail.StreamGuard

	mu         sync.Mutex
	responseID string
	model      string
	content    strings.Builder
	stopReason string
	usage      *interaction.UnifiedUsage
	final      bool
}

func newStreamCollector(out contracts.StreamWriter, gr *gatewayguardrail.StreamGuard) *streamCollector {
	return &streamCollector{out: out, gr: gr}
}

func (c *streamCollector) WriteChunk(ctx context.Context, chunk contracts.StreamChunk) error {
	event := chunk.Event
	c.mu.Lock()
	if event.ID != "" {
		c.responseID = event.ID
	}
	if event.Model != "" {
		c.model = event.Model
	}
	if event.StopReason != "" {
		c.stopReason = event.StopReason
	}
	if event.Usage != nil {
		usage := *event.Usage
		c.usage = &usage
	}
	if event.Final {
		c.final = true
	}
	c.mu.Unlock()
	delivered := event.Delta
	if c.gr != nil {
		// The three-tier guard polices the text delta before delivery. The
		// buffered layer may hold the text across chunks and release a redacted
		// window; a block error cuts the stream (the executor aborts and the
		// request is finalized as a guardrail failure).
		var err error
		delivered, err = c.gr.Write(ctx, event.Delta, event.Final)
		if err != nil {
			return err
		}
		if event.Final {
			c.gr.Stop()
		}
	}
	// Only text the guardrail released reaches both the assembled response and
	// the client, so the final response always matches what was delivered.
	if delivered != "" {
		c.mu.Lock()
		c.content.WriteString(delivered)
		c.mu.Unlock()
	}
	if c.out != nil && (delivered != "" || event.StopReason != "" || event.Final || len(event.ToolCallDeltas) > 0) {
		rewritten := chunk
		rewritten.Event.Delta = delivered
		return c.out.WriteChunk(ctx, rewritten)
	}
	return nil
}

// Response assembles the unified response from the collected events.
func (c *streamCollector) Response(now time.Time) *interaction.UnifiedResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	usage := interaction.UnifiedUsage{}
	if c.usage != nil {
		usage = *c.usage
	}
	stopReason := c.stopReason
	if stopReason == "" && c.final {
		stopReason = "stop"
	}
	return &interaction.UnifiedResponse{
		ID:         c.responseID,
		Model:      c.model,
		StopReason: stopReason,
		Usage:      usage,
		CreatedAt:  now,
		Choices:    []interaction.Choice{{Message: interaction.Message{Role: "assistant", Content: c.content.String()}}},
	}
}
