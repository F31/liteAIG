# Phase 4 — ClickHouse Analytics Sink: DoD Evidence

Status: Complete (9/9 tasks)

This change adds a replaceable, buffered, idempotent analytics sink with a
ClickHouse-compatible HTTP backend, forwarding redacted Domain Events off the
request hot path.

## Capabilities Delivered

### Analytics Sink (`analytics-sink`)
- `internal/observability/sink`: `AnalyticsSink` contract (`Write`,
  `Flush`), `AnalyticsEvent` (stable id), and a `BufferedSink` with batch
  size/interval flush, bounded backlog, retry with backoff, and drop-oldest
  under pressure.
- `ClickHouseWriter`: HTTP JSONEachRow insert writer.
- `Adapter`: forwards `contracts.DomainEvent` to the sink with redaction
  (whitelisted fields only; no bodies/secrets).

### Modified Capability
- `observability-and-alerting`: Domain Events can be forwarded to an optional
  buffered analytics sink without coupling business modules.

## Gate Results

| Gate | Result |
| --- | --- |
| `go test -race -timeout 180s ./...` (Postgres 55432 + Redis 56379) | PASS |
| `go vet ./...` | PASS |
| `go run ./cmd/architecture-test` | PASS |
| `actionlint` | PASS |
| `gitleaks --config .gitleaks.toml detect` | no leaks found |
| `openspec validate --all --strict` | 21 passed, 0 failed |

## Tests

- Buffered sink: flush on batch size, flush on interval, retry with backoff
  (acknowledged events not re-sent), backlog pressure drops oldest and records it.
- Adapter: whitelisted attributes retained; `content`/`authorization` never
  leak into analytics.
- ClickHouse writer: HTTP JSON insert received by a test server.

## Scope Note

The buffered sink, ClickHouse writer, and redaction adapter are fully
implemented and tested. Wiring the optional adapter into a specific runtime
composition (which backend to forward to) is a thin operator configuration; the
contracts and metrics interface are delivered. Kafka-compatible sink and
data-residency routing remain out of scope.
