# The activity timeline

The **Activity** page (`/app/activity`) is Bright Guard's invocation log. It
shows one row per MCP capability invocation, with caller identity, the
capability invoked, status, latency, and which policies (if any) matched.

This page covers the chip vocabulary, the filters, and the underlying data
model.

## Filters

Three filter sets work in combination:

- **Time window** — last hour, last 24h, or last 7d. The summary block at
  the top recomputes accordingly. Default is 24h.
- **Kind** — `tool`, `resource`, `prompt`. Multi-select. None checked means
  all kinds are visible.
- **Status** — `ok`, `error`, `denied`. Multi-select; same semantics.

The free-text **search** box matches against capability name and caller
agent. Search is debounced — give it a beat after typing.

## Status chips

Each row's status column shows one or two chips:

| Chip | What it means |
|---|---|
| `ok` (green) | The MCP server returned a successful response. |
| `error` (red) | The MCP server returned an error. Bright Guard didn't intervene. |
| `DENIED` (red, uppercase) | The invocation was blocked. Either the capability is currently disabled in **MCP Servers**, or a `deny`-action policy matched. |

When a policy matches an invocation, a second chip appears next to the
status:

| Secondary chip | What it means |
|---|---|
| `ENFORCED by <policy>` (red) | The named policy fired with `deny` action and the gateway actually blocked the call. Status is `DENIED`. |
| `by <policy>` (amber) | A policy matched but did not block. Either it had `warn` action, or the invocation was already denied by an earlier source. Reading: "would have been blocked / warned". |

In short: **ENFORCED** means action was taken; the amber chip means a
counterfactual ("here's what your policy would have done").

## How decisions are computed

Each gateway evaluates the active policy bundle in-process for every
invocation it sees. If a `deny` policy matches it flips `status` to
`denied` before sending the observation to the control plane. The
control plane stores the decision list verbatim — it does **not**
re-evaluate, except by the background sweep that picks up policies added
*after* an observation was recorded (so brand-new rules can light up
historical activity with amber chips).

Two consequences worth knowing:

1. **An offline shim can't be tricked.** Even if a malicious shim
   suppresses denied decisions in its observation payload, the control
   plane will replay the policies against the recorded fields and add the
   amber chip back.
2. **A renamed or deleted policy keeps its chip.** Decisions are stored
   with the snapshot of the policy that produced them, not as a foreign key
   into a mutable table.

## Caller identity

The **Caller** column shows the JSON-encoded `caller` object the shim
forwarded, truncated to ~80 characters. The most common shape is
`{"agent": "<some-bot>"}`, but the gateway can attach any string-keyed map
— Bright Guard treats it as opaque metadata.

If you need to dig deeper, click into the **Callers** page in the left rail
to see one row per distinct `agent` value with last-seen and total-invocation
counts.

## API access

The same data is available over the API:

- `GET /api/orgs/{orgId}/activity` — paginated list, supports cursors,
  filters mirror the SPA's. See the [route reference](reference/routes.md).
- `GET /api/orgs/{orgId}/activity/summary` — bucketed counts for the chart.

Use the [CLI device flow](cli/device-flow.md) to get a bearer token, then:

```sh
curl -H "Authorization: Bearer $BG_TOKEN" \
  "https://mcp-governance.infoblox.dev/api/orgs/$ORG_ID/activity?windowMs=86400000&status=denied"
```

## Retention

Activity rows are retained for 30 days. The summary buckets are persisted
separately and follow the same window; the rolled-up totals on the
**Overview** page are sourced from the same store. Long-term archival to
BigQuery is on the roadmap but out of scope today.
