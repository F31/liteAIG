# Production Trial Monitoring Checklist

Single-tenant, non-critical traffic, ≥2 week observation period. Standard tier (Postgres + Redis coordination, ≥2 replicas, ≥2 AZ). All items below are checked against the live deployment during the trial.

## 1. Go / No-Go Gates (checked before any production traffic is admitted)

| Gate | How to verify | Pass criteria |
|---|---|---|
| Latest commit CI green | GitHub Actions `ci` + `sdk-conformance` on the deployed commit | Both green |
| Release artifacts verified | GHCR image pullable + SBOM attached + SPDX valid | All present |
| Load benchmark within SLO | `cmd/loadbench` against the deployed topology | `chat P99 < 30ms`, `stream TTFT P99 < 30ms`, `error rate < 1%` |
| Backup + restore rehearsed | `scripts/backup-liteaig.sh` + `scripts/restore-liteaig.sh` round-trip on the trial DB | Restore completes and app boots |
| Pod-replacement recovery | Kill one replica, confirm the other serves uninterrupted | No request errors, <5s recovery |
| All default alerts imported | `POST /api/admin/alerts/rules/import-defaults` applied | Rules visible in Console |

## 2. Golden Signals (dashboards / alerts)

### Data plane

| Signal | Source | Threshold | Action on breach |
|---|---|---|---|
| HTTP 5xx rate | `/metrics` or LB | >1% sustained for 5 min | Investigate provider health / upstream connectivity |
| HTTP 4xx rate | `/metrics` or LB | >10% sustained | Check key validity, tenant config, rate limits |
| p99 latency | `cmd/loadbench` periodic / LB | >100 ms sustained | Check Postgres write path, Redis coordination, provider latency |
| Gateway RPM saturation | `/metrics` or LB | >80% of `--gateway-rpm` | Raise limiter or scale replicas |
| Error budget burn | SLO tracking | >0.5% error budget consumed in 24 h | Freeze release, root-cause |

### Control plane / security

| Signal | Source | Threshold | Action |
|---|---|---|---|
| `admin.security.login_failed` | OTLP logs | >5 / 5 min | Review source, check for brute-force |
| `admin.security.rate_limited` | OTLP logs | >0 for legit admin | Investigate session/IP |
| `admin.security.reauth_failed` | OTLP logs | >3 / 5 min | Review account compromise possibility |
| `admin.security.reauth_required` | OTLP logs | Unexpected spike | Audit which admin actions are being attempted |

### Storage / coordination

| Signal | Source | Threshold | Action |
|---|---|---|---|
| Postgres connections | `pg_stat_activity` | >80% of max | Scale pool or investigate leak |
| Postgres replication lag | `pg_stat_replication` | >30 s | Failover readiness check |
| Redis memory | `INFO memory` | >80% of max | Check coordination key growth |
| Coordination lease contention | `platform:*` lease logs | Frequent owner flapping | Check network partition / clock skew |
| `liteaig_event_outbox_events{status=failed}` | `/metrics` | >0 sustained | Check webhook sink health |

### Audit / compliance

| Signal | Source | Threshold | Action |
|---|---|---|---|
| `liteaig_audit_events_purged_total` | `/metrics` | Non-zero after retention window | Confirm retention sweep is running |
| `liteaig_audit_purge_last_timestamp_seconds` | `/metrics` | >2 h stale | Check retention loop health |
| `liteaig_file_mappings_purged_total` | `/metrics` | Unexpected jump | Review file mapping retention policy |

## 3. Operational Runbooks to Keep on Hand

- `docs/DEPLOYMENT.md` — topology and configuration reference
- `docs/RELEASE.md` — upgrade operator checklist
- `docs/UPGRADE_DRILL.md` — migration and rollback rehearsal
- `docs/DR_RUNBOOK.md` — RPO/RTO, SQLite/Postgres recovery steps
- `docs/A2A_TRUSTED_RELEASE.md` — A2A conformance and outbox boundaries
- `docs/GATEWAY_BENCHMARK.md` — capacity baselines (local smoke + Standard tier)

## 4. What This Trial Does NOT Cover

- Multi-AZ traffic switch / failover drill (A5) — not yet automated
- Cloud object-store credential rotation (C2) — not yet implemented
- OIDC-native step-up reauth (C5) — fail-closed for SSO-only sessions
- Multi-tenant SaaS GA (A4) — only single-tenant in this trial
- Registry publishing with signed provenance (A3) — chain verified without signing
