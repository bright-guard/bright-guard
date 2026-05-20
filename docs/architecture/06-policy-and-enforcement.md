# Policy & Enforcement

Bright Guard's policy primitive is a CEL expression returning `bool`. One expression is stored per `policies` row (`cloud/api/internal/db/migrations/00013_policies.sql:9-10`). Two evaluators exist — one in the control plane, one in the shim — and they share an environment declaration that is **byte-for-byte identical** so the same expression yields the same verdict on either side.

## CEL environment

Five variables, no extensions:

```go
cel.Variable("caller",     cel.MapType(cel.StringType, cel.DynType))
cel.Variable("server",     cel.MapType(cel.StringType, cel.StringType))
cel.Variable("capability", cel.MapType(cel.StringType, cel.StringType))
cel.Variable("at",         cel.TimestampType)
cel.Variable("status",     cel.StringType)
```

Server: `cloud/api/internal/policy/policy.go:36-42`. Shim: `cloud/shim/cmd/shim/policy.go:36-42`.

Notably, `cel.ext.Strings()` is deliberately not enabled — its regex helpers can be DoS'd via backtracking on tenant input (`cloud/api/internal/policy/policy.go:5-8`). Eval is bounded by a hard cost limit of `50_000` (`cloud/api/internal/policy/policy.go:26`); same value in `cloud/shim/cmd/shim/policy.go:16`.

Example expression that denies any tool invocation by `caller.agent == "demo-agent"` on capability `delete_*`:

```cel
caller.agent == "demo-agent" && capability.kind == "tool" && capability.name.startsWith("delete_")
```

## Storage

| Column on `policies` | Meaning |
|---|---|
| `expression` | The CEL source. Compiled at evaluation time (sweep) and at create/update time for validation. |
| `action` | `deny` or `warn`. Affects what the shim does with a match (deny flips `status` to `denied`; warn just records the decision). |
| `enabled` | If false, policy is invisible to the sweep + bundle. |
| (trigger) | Any insert/update/delete bumps `orgs.policy_bundle_version` via `bump_org_policy_bundle_version` (`cloud/api/internal/db/migrations/00014_policy_bundle_version.sql:5-16`). |

Decisions live in `mcp_invocation_decisions(invocation_id, policy_id)`. Only matched=true rows are persisted today.

## Distributed enforcement (Wave N+5 — UC10 first slice)

The shim asks for the bundle on every heartbeat (`cloud/shim/cmd/shim/main.go:258-286`):

```
POST /v1/gateway/heartbeat
Authorization: Bearer <credential>
X-Bundle-Version: <last applied version, or 0>
```

The handler (`cloud/api/internal/api/phase2.go:347-369`) loads the org's `policy_bundle_version`; if it's strictly greater than the shim's cached version, the response includes the full bundle. Otherwise the bundle is omitted to keep ticks cheap. Bundle delivery is a **pull from the control plane on every heartbeat**, not push.

On the shim, `policyCache.apply` (`cloud/shim/cmd/shim/main.go:390-409`) is fail-closed: it compiles every policy in the new bundle first; on any compile error it logs and **keeps the previous bundle live**. This means a control-plane operator who ships a malformed policy cannot break enforcement at the customer site — the bundle simply doesn't roll forward there.

The shim evaluates every loaded program against every fake invocation it generates (`cloud/shim/cmd/shim/main.go:209-220`). If a `deny`-action policy matches, the shim flips `status="denied"` on the outgoing observation and attaches a `decisions[]` array. Observations carrying decisions cause the server to insert `mcp_invocation_decisions` alongside `mcp_invocations` in the same transaction (`cloud/api/internal/api/phase2.go:462-470`).

## Server-side sweep (audit-only path)

For shims that don't have a bundle loaded — or for any invocation that arrived without decisions — the `bg-policy-sweep` scheduler evaluates the same engine server-side (`cloud/api/internal/scheduler/policy_sweep.go`):

- Runs every 30s (`cloud/api/internal/scheduler/policy_sweep.go:17`).
- Single-leader via `pg_try_advisory_lock(hashtext('bg-policy-sweep'))`.
- Per-org watermark (`policy_sweep_state.watermark`) advances forward only; the sweep never re-scans history.
- Limits: 50 orgs per tick, 1000 invocations per org per tick.
- Compiled programs are cached for the duration of a tick.
- Invocations that already have a row in `mcp_invocation_decisions` are skipped (`ListInvocationsForSweep` filters them).

The sweep is pure audit. It never modifies `mcp_invocations.status`. Today, **the only path that flips status to `denied` is the shim's local eval** (the disabled-capability denylist, also delivered on heartbeat, is the other deny source).

## Simulate endpoint

`POST /api/orgs/{orgId}/policies/{id}/simulate` runs the engine against a recent window of invocations and returns a count of matches without persisting anything. Backs the "would-have-matched-X" affordance on `PolicyDetailPage`. Source: `cloud/api/internal/api/policies.go` + `cloud/api/internal/scheduler/policy_sweep.go:196-240` (`BackfillPolicy` shares compile/eval code).

## Gotchas

- **The shim's CEL env must match the server's exactly.** Adding a variable, an extension, or a different cost limit on one side without the other introduces silent verdict drift. The two files cross-reference each other with explicit "byte-for-byte" comments — keep them honest.
- **Eval errors are non-matches.** Missing-key access (`caller.agent` on a row where `caller` has no `agent`) or cost-limit-exceeded both fall through silently (`cloud/api/internal/scheduler/policy_sweep.go:161-167`). This is intentional — the alternative would be writing "decision: error" rows that the activity view doesn't surface. The cost: a typoed policy looks like "no matches" rather than failing loudly.
- **Watermark is per-org.** A noisy org cannot starve a quieter one because `ListOrgsWithDuePolicies` paginates over orgs, not over time. But within an org, the sweep is single-threaded — 50k invocations behind the watermark needs ~50 ticks (25 minutes) to catch up at default limits.

## What "enforcement" actually does today

It changes a row's `status` field. Nothing on the data path is blocked. The vision (UC10) is for the shim to be a real-world agentgateway sidecar that returns 403 to the calling agent; today the shim is a demo emitter, so "denied" means "would-have-been-blocked." The operating-plan calls this the "first slice" of UC10; the second slice (real agentgateway integration) is on the roadmap (see [12-tradeoffs-and-roadmap.md](./12-tradeoffs-and-roadmap.md) and GH epic #13).
