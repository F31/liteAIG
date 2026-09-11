# Phase 4 — ClickHouse Analytics Sink: Design

## Context

Business modules emit `contracts.DomainEvent` through the `EventSink` contract
(`internal/kernel/contracts/event.go`). This change adds an optional buffered
Analytics Sink that forwards redacted events to a ClickHouse-compatible HTTP
insert endpoint without coupling modules to ClickHouse.

## Goals / Non-Goals

**Goals:**
- An `AnalyticsSink` contract + `ClickHouseSink` (HTTP JSON insert, buffered
  batches, retry, backpressure).
- Batch flush on size/interval; bounded retry with backoff; drop-oldest on a
  bounded backlog under pressure.
- Idempotent batch writes (per-event id) so retried batches do not duplicate rows.
- Redaction: analytics payloads exclude prompt/response bodies and secrets.
- Metrics through the existing MetricSink.

**Non-Goals:**
- Kafka sink, data-residency routing, Multi-AZ/DR, gRPC Extension Bridge,
  Phase 5.

## Decisions

### 1. AnalyticsSink is a contract; business modules stay on EventSink
New `internal/observability/sink`: `AnalyticsSink` with `WriteBatch(ctx, []Event)`
and `Flush(ctx)`. A small adapter forwards `contracts.EventSink` emissions into
the buffered sink; business modules never import ClickHouse.
Alternative considered: business modules call ClickHouse directly. Rejected —
violates the event-contract rule.

### 2. Buffering with flush on size/interval and backpressure
`ClickHouseSink` buffers events, flushes on `BatchSize` or `FlushInterval`,
retries a failed flush with bounded backoff, and under a bounded backlog drops
the oldest events (metric + counter). This keeps the hot path non-blocking and
bounded.

### 3. Idempotent batches
Each analytics event carries an `id`; a batch insert is idempotent so a retried
batch does not duplicate rows (ClickHouse ReplacingMergeTree-style dedupe is the
operator's option; the sink guarantees stable ids and no re-send of acknowledged
batches).

### 4. Redaction by construction
The adapter copies only whitelisted `DomainEvent` fields (kind, scope, request
id, timestamps, low-cardinality attributes) into the analytics payload; bodies
and secret-like attributes are never copied.

## Risks / Trade-offs

- [Backpressure] → bounded backlog + drop-oldest + metrics.
- [Retry duplicates] → idempotent batches with stable ids; no re-send of acknowledged batches.
- [Latency on hot path] → buffered, non-blocking; flush off the request path.

## Migration Plan

1. Add `internal/observability/sink` (contract, buffer, ClickHouse HTTP writer, retry/backpressure, redaction) with tests.
2. Wire an optional `EventSink` adapter forwarding to the buffered sink.
3. Add metrics (buffered/backlog/dropped) and a redaction test.
4. Run full gate set + OpenSpec strict validation + record DoD evidence.

Rollback: the sink is optional; without configuration the EventSink path is
unchanged.
