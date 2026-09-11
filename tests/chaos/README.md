# Phase 1 Chaos Gates

- Redis outage: hard Budget fails closed, soft Budget fails open with degraded state.
- PostgreSQL outage: loaded immutable RuntimeSnapshot remains available to the Data Plane.
- Provider faults: Golden Scenario B injects 429, 5xx, timeout/slow TTFT, credential exhaustion, and circuit-open routing.
- Accounting store outage: provider-produced usage persists in the local Spool WAL and replays to zero duplicate rows after recovery.
