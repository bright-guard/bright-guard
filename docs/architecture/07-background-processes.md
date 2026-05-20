# Background Processes

The API binary runs four long-lived sweep goroutines, launched from `cloud/api/cmd/api/main.go:99-120`. They coordinate across replicas via Postgres advisory locks. There is one leader at a time per sweep type; non-leaders idle until the next tick.

## The four (soon five) sweeps

| Sweep | Lock key | Default interval | Source |
|---|---|---|---|
| Discovery (direct MCP connections) | `bg-discovery` | 60 min (override: `DISCOVERY_INTERVAL_MINUTES`) | `cloud/api/internal/scheduler/scheduler.go:19` |
| Exposure classifier | `bg-exposure-sweep` | 10 min (override: `EXPOSURE_SWEEP_INTERVAL_MINUTES`) | `cloud/api/internal/scheduler/exposure_sweep.go:13` |
| Caller sweep | `bg-callers` | 5 min (hardcoded) | `cloud/api/internal/scheduler/caller_sweep.go:15` |
| Policy sweep | `bg-policy-sweep` | 30 s (hardcoded) | `cloud/api/internal/scheduler/policy_sweep.go:16-17` |
| Daily-metrics rollup | `bg-metrics-rollup` | _(landing in current wave)_ | `cloud/api/internal/scheduler/metrics_rollup.go`; populates `org_daily_metrics` for the executive-dashboard OverviewPage. **Uncommitted in worktree at the time of writing.** |

## The advisory-lock pattern

All four use the same wrapper:

```go
ok, err := Connections.TryAdvisoryLock(ctx, key)
```

which runs `select pg_try_advisory_lock(hashtext($1)::bigint)` (`cloud/api/internal/store/connections.go`). The lock is session-scoped — released when the pool connection returns to the pool (which happens on every tick because each tick borrows + releases a connection). This is deliberate: a crashed-mid-tick replica releases the lock when its TCP connection drops, so the next tick's leader election just works.

**Distinct keys per sweep are critical.** If two sweeps shared a key they would silently serialize. The keys above are unique by construction; see project `CLAUDE.md` for a "reserve advisory-lock keys" wave-coordination rule.

## Discovery sweep

Re-runs the MCP introspection sequence (`Initialize → ListTools → ListResources → ListPrompts`) against every direct-mode `mcp_connections` row whose `last_discovered_at` is older than the stale TTL (6 hours, `cloud/api/internal/scheduler/scheduler.go:20`). Updates `mcp_servers` + `mcp_capabilities` on success. OAuth2 connections with `oauth_status != authorized` are skipped (no token to send). Failures land in `mcp_connections.last_error`.

Method-not-found errors (JSON-RPC `-32601`) on `tools/list`, `resources/list`, or `prompts/list` are treated as "this server has none of that kind" rather than as a failure — many MCP servers implement only one of the three (`cloud/api/internal/scheduler/scheduler.go:221-226`).

## Exposure sweep

URL-only classifier (no DNS, no probes). Walks `mcp_servers` rows whose `exposure_classified_at` is older than 24 hours (`cloud/api/internal/scheduler/exposure_sweep.go:14`). Calls `exposure.Classify(addr)` (`cloud/api/internal/exposure/classify.go:55`) and writes back the resulting state + reason. Five states: `unknown | internal | cloud_internal | public | unreachable`. Active reachability probing is on the roadmap.

## Caller sweep

Maintains `org_callers` — a per-org dedup of distinct invocation callers. Two passes per tick:

1. `SweepNew` — scans `mcp_invocations` since the last sweep mark (with a 1-minute lookback buffer) and upserts a row keyed by `(org_id, signature)` where signature is a canonical hash of the caller JSON. Sets `flagged_new=true` on first sight.
2. `FlagAgeRollover` — clears `flagged_new` on rows older than 7 days (`callerFlagAge`).

`flagged_new` drives the "NEW" chip in the callers UI.

## Policy sweep

Audit-only evaluation of `mcp_invocations` against enabled CEL policies. Per-org watermark via `policy_sweep_state`. See [06-policy-and-enforcement.md § Server-side sweep](./06-policy-and-enforcement.md#server-side-sweep-audit-only-path) for the full description.

## Coordinating across replicas

Cloud Run can scale this service from 0 to 4 instances (`cloud/deploy/deploy.sh:53`). All replicas run all four schedulers. Advisory locks ensure that for any given tick, exactly one replica does the work; the others get `ok=false` from `TryAdvisoryLock` and quietly return. Because the lock is released when the connection drops (or when the pool connection is returned), there is no "stuck lock" failure mode — a wedged replica will lose its lock the moment its pool connection times out.

## What's intentionally *not* a sweep

- Every shim heartbeat triggers `TouchSeen` + `CommitEnrollmentOnHeartbeat` inline. No sweep is needed to update gateway last-seen state.
- `mcp_invocation_decisions` inserts happen inline on `/v1/gateway/observations` when the shim shipped decisions; the sweep only handles "no shim eval" cases.
- Session cleanup is not automated. Expired session rows stay in the table until the next time something reads them. There is no `delete from sessions where expires_at < now()` job. This is a small bit of tech debt; see [10-observability.md](./10-observability.md#open-gaps).

## Gotcha · hardcoded intervals

Three of the four sweeps have **hardcoded intervals**. Only the discovery and exposure sweeps respect env overrides. If you need to tune the caller or policy sweep cadence in production, that's a code change. Often fine — these defaults have held — but worth knowing.
