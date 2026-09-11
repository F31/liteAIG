## 1. Analytics Sink Contract and Buffer

- [x] 1.1 Add `internal/observability/sink`: `AnalyticsSink` contract (`WriteBatch`, `Flush`) and an `AnalyticsEvent` type carrying a stable id.
- [x] 1.2 Add a buffered implementation with batch size/interval flush, bounded backlog, retry with backoff, and drop-oldest under pressure.
- [x] 1.3 Add tests: flush on size, flush on interval, retry with backoff, backlog pressure drops oldest and records it.

## 2. ClickHouse HTTP Writer and Redaction

- [x] 2.1 Add a ClickHouse-compatible HTTP JSON insert writer behind the sink.
- [x] 2.2 Add an adapter that copies only whitelisted `contracts.DomainEvent` fields into redacted analytics payloads (no bodies/secrets).
- [x] 2.3 Add tests: idempotent retried batch (acknowledged events not re-sent, stable ids), payload contains no prompt/response/secret.

## 3. Wiring and Release Gates

- [x] 3.1 Wire an optional `EventSink` adapter forwarding to the buffered sink; metrics (buffered/backlog/dropped) through the MetricSink.
- [x] 3.2 Run the full gate set (`go test -race ./...`, `go vet ./...`, architecture-test, `npm run check`, actionlint, gitleaks) and resolve blocking failures.
- [x] 3.3 Run `openspec validate --all --strict` and record Phase 4 analytics-sink DoD evidence.
