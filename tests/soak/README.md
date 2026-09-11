# Phase 1 Soak Harness

The harness continuously reserves/reconciles budget and acquires/releases leases, then proves full budget and lease capacity remain available.

Smoke gate:

```bash
go test ./tests/soak -count=1 -v
```

24-hour Redis release run (MUST use a dedicated Redis instance):

```bash
LITEAIG_TEST_REDIS_ADDR=localhost:56381 \
LITEAIG_SOAK_DURATION=24h \
LITEAIG_SOAK_INTERVAL=100ms \
go test ./tests/soak -run TestPhase1SoakHarness -count=1 -timeout=25h -v
```

## Dedicated instance requirement

The 24-hour soak MUST run against a Redis instance that no other test touches.
The harness flushes its target DB on start, and other suites also call
`FlushDB`; sharing a Redis instance with the full `go test ./...` run corrupts
the soak's reservation state and produces false "reservation leak" failures.
Use a dedicated port (e.g. `56381`) for the long run and keep all other test
runs on a different instance (e.g. `56379`). A short accelerated soak
(`LITEAIG_SOAK_INTERVAL=1ms`, 10 minutes) is the fast development gate; the
full 24-hour run is the release evidence.
