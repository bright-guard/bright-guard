# Observability

Honest section. There is very little here.

## What is in place

- **Health endpoint.** `GET /api/healthz` always returns `{"ok":true}`. Used as the Cloud Run startup probe. (`cloud/api/internal/api/server.go:59-61`.) The `/healthz` no-prefix path hits the GFE 404 page — a real bug we ate once; use `/api/healthz`.
- **Per-request logs.** chi's `middleware.Logger` writes a line per HTTP request with method, path, status, duration (`cloud/api/internal/api/server.go:54`). These show up in Cloud Logging tagged to the `bright-guard` Cloud Run service.
- **Handler error logs.** Every error path in handlers calls `log.Printf` before returning. The Wave N+1 commit (`9c38f04`) made these consistent across all handlers.
- **Migration log.** `migrations applied` is logged at process boot once migrations run cleanly (`cloud/api/cmd/api/main.go:56`).
- **Auth-event logs.** Logout, session-delete, OAuth init/callback all log. No structured event stream — just `log.Printf`.
- **Scheduler logs.** Each sweep logs on advisory-lock failure, list failure, and per-item failure. Successful ticks are silent.
- **Shim logs.** `tick: heartbeat ok, bundle_version=N, K servers, M invocations` per tick (`cloud/shim/cmd/shim/main.go:253`); bundle apply logs version + program count.
- **Platform audit log.** Every platform-admin write is recorded in `platform_audit_log` with structured `details` JSON. This is the only audited write path in the system.

## What is in the data layer

- **Append-only `mcp_invocations`** with `at`, `status`, `latency_ms`. Activity UI pages by `at desc` with cursor pagination.
- **Append-only `mcp_invocation_decisions`** keyed `(invocation_id, policy_id)`.
- **`gateways.last_seen_at`** advanced inline by every heartbeat.
- **`org_callers`** with `first_seen_at`, `last_seen_at`, `invocation_count`, `flagged_new`.

These are application-layer telemetry tables, not observability infrastructure. They are how customers see the product, not how operators see the platform.

## Open gaps

- **No OpenTelemetry export.** No traces, no metrics emitted to an external collector. Phase-2 of the operating-plan calls out SIEM / OTel export as enterprise-hardening; it has not landed.
- **No structured logs.** `log.Printf` lines are free-text. Cloud Logging parses them as a single textPayload field. Filtering on `"successfully migrated"` or `"observations"` is by substring, not field.
- **No request ID propagation past the entrypoint.** chi sets a request ID (`middleware.RequestID`) but it is not added to the log line format, and handlers do not echo it. Debugging a flaky request requires guessing by timestamp.
- **No metrics endpoint.** No `/metrics` Prometheus surface. No counters on policy-sweep latency, no histograms on shim-ingest size, no error-rate gauges.
- **No SLO definition, no error budget.** Today the only signal of trouble is `/api/healthz` returning non-200, which Cloud Run's monitoring catches.
- **No session-row reaper.** Expired session rows accumulate in `sessions` indefinitely. Cost: small. Effect: `sessions` table grows monotonically per signed-in user, ~1 row per device-flow approval per CLI session.
- **No invocation retention.** `mcp_invocations` grows without bound; no partition, no rollup other than the new `org_daily_metrics` (in flight). At any meaningful customer volume this becomes the first table to need partitioning.
- **No alerting beyond Cloud Run defaults.** Cloud Run will alert on revision-rollout failures and instance crashes via the project's monitoring config; nothing is wired to PagerDuty.

## Where this is on the roadmap

Operating-plan §03 lists "Backend Engineer — Enterprise Integrations" as the role that owns OTel export, retention/partitioning, SIEM integration. It is explicitly an incremental investment, not foundation. Internal engineers: do not assume any of the gaps above will be silently fixed; they need a wave of their own.

## How to investigate a production issue today

1. `gcloud logging read 'resource.labels.service_name=bright-guard' --limit=50 --order=desc` — broad latest log.
2. Filter by substring: `textPayload:"policy sweep:"` for sweep issues.
3. `gcloud run services describe bright-guard --region=us-central1` for current revision + env vars.
4. SQL spelunking via `gcloud sql connect bright-guard-db --user=brightguard` (rare; usually the SPA's data is enough).
5. For shim issues, the demo shim's logs are at `bright-guard-shim-demo`. Customer shims are customer-owned.

This is workable for one engineer and one production tenant. It will not scale to support-engineer-on-rotation.
