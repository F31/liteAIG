# Phase 4 — ClickHouse Analytics Sink

## Why

V8.2 §1.3/§21 make ClickHouse-compatible analytics an optional Enterprise sink for high-volume analytics, not a request hot-path dependency. The observability layer already emits standard Domain Events via the `contracts.EventSink` contract; this change adds a replaceable Analytics Sink that batches redacted events and writes them to a ClickHouse-compatible HTTP insert endpoint, with backpressure and idempotent batch writes, without coupling business modules to ClickHouse.

## What Changes

- Add an `AnalyticsSink` contract (batch write of redacted analytics events) behind the existing event contract; a `ClickHouseSink` implementation uses the HTTP insert endpoint with buffered batches, retry, and backpressure.
- Batch policy: buffer events, flush on batch size or interval, retry on failure with bounded backoff, drop-oldest on a bounded backlog under pressure.
- Idempotent batch writes: each event carries an id so a retried batch does not duplicate analytics rows.
- Redaction: analytics payloads exclude prompt/response bodies and secrets by construction.
- Metrics: buffered/backlog/dropped counters through the existing MetricSink.

## Capabilities

### New Capabilities
- `analytics-sink`: replaceable, buffered, idempotent analytics sink with a ClickHouse-compatible HTTP backend.

### Modified Capabilities
- `observability-and-alerting`: domain events can be forwarded to a buffered analytics sink without coupling business modules to the concrete backend.

## Impact

- **Backend**: new `internal/observability/sink` (contract, buffer, ClickHouse HTTP writer, retry/backpressure), wired optionally behind the EventSink; metrics + redaction.
- **Dependencies**: no new third-party runtime dependency (HTTP JSON insert; ClickHouse itself is optional infrastructure).
- **Tests**: batch flush on size/interval, retry on failure with backoff, backlog pressure drops oldest, idempotent retried batch (no duplicate rows), redaction (no bodies/secrets in payload), metrics reach the sink.

### Non-Goals
- Kafka-compatible sink, data-residency routing, Multi-AZ/DR orchestration, gRPC Extension Bridge, Phase 5 (separate remainder items).

**Golden Scenario:** strengthens Scenario G (analytics decoupled from the hot path) and observability (§21 metrics).
