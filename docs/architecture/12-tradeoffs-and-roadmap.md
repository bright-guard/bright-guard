# Tradeoffs and Roadmap

This section is for engineers and architects who want to understand *what shipped* versus *what the vision says* — and where the difference matters.

## The big one: gateway-based vs DNS-passive discovery

`vision.md` calls out DNS as the **primary signal** for MCP server discovery (UC1): "DNS — known MCP service names, lookup patterns, and resolution behavior." Augmenting signals are HTTP / proxy telemetry, workload metadata, third-party feeds.

**What ships today is gateway-based discovery.** The customer deploys a shim (or, less often, configures a direct `mcp_connections` row) that pushes inventory and observations into the control plane. There is no DNS telemetry pipeline. Operating-plan §06 calls this out as the single biggest vision gap:

> *"Gateway-based discovery does not fulfill the network-native pillar. Vision text describes DNS as the primary signal. Today's discovery requires the customer to deploy a gateway."*

The mitigation in sales framing is "gateway-first, DNS-augmented" until DNS-passive ships. The mitigation in code is a planned cross-team contract with the Infoblox DDI / Network Telemetry team for ~10% × 90 days (operating-plan §02). GH issue tracking exists at the epic level (#2 — UC1).

For an architect evaluating Bright Guard: this matters because the product's strategic moat ("network-native, cross-tenant DNS baseline") is **roadmap, not code**. The shipping product is a competent gateway-based MCP governance plane; that is a different category from a DNS-native control plane.

## Shim CEL eval vs real agentgateway integration

Wave N+5 shipped the "first slice" of UC10 (distributed enforcement). The shim:
- Embeds `cel-go`.
- Pulls the policy bundle on every heartbeat.
- Evaluates locally.
- Ships decisions back with observations.

**What's a real-world enforcement agent it isn't (yet):**
- It is a **demo emitter**, not a proxy. Today the shim invents synthetic invocations from `examples/fake-servers.yaml`; it does not intercept real MCP RPC. There is no inline blocking; "denied" means "would-have-been-blocked."
- It is **not a sidecar** to agentgateway, Runlayer, or MintMCP. Integration with these is on the operating-plan as Phase-3 work and on the roadmap as epic #13.
- Bundles are **not signed.** A compromised control plane could push attacker-authored CEL. Bundle signing is on the same epic.

The architecture *contract* — heartbeat carries bundle, shim evaluates locally, observations carry decisions, sweep skips decided rows — is exactly the shape a real agentgateway integration would use. The CEL env declaration, cost limit, and verdict semantics are stable. What changes when a real proxy lands is what the shim *does* with a `deny` verdict (return 403 to the calling agent vs flip `status`).

## Audit-only policies vs enforced policies

`mcp_invocations.status` can be `denied` only by:
1. The disabled-capability list (control plane → heartbeat → shim filters).
2. The shim's local CEL eval flipping status when a `deny` policy matches.

Both happen at the shim. The control plane's policy sweep is **audit-only** — it writes `mcp_invocation_decisions` but never modifies `mcp_invocations`. This means:

- For a customer whose shim is up to date (`bundleVersion >= server version`): enforcement is live.
- For a customer whose shim is behind, mis-configured, or absent: enforcement is *audit only* and decisions show up in the activity timeline minutes-to-hours later.

This is consistent — same engine, same answer — but the latency story is different. An architect needs to know that today's "deny" is local-deny-at-shim, not global-deny-at-control-plane.

## What's not started

From operating-plan §05, unshipped use cases:

| UC | Status | GH epic |
|---|---|---|
| UC5 — Policy simulation | Not started; design pending | #18 |
| UC6 — Asset & user policy | Not started; CEL env audit needed | #16 |
| UC7 — Network-aware enforcement | Not started | #17 |
| UC11 — Unified policy console | Not started | #19 |

And partially-shipped:

| UC | What's there | What's missing | GH epic |
|---|---|---|---|
| UC4 — Policy authoring | CEL audit-only; PolicyDetailPage; simulate endpoint | Versioning, revert, decision-audit linkage | #12 |
| UC8 — Exposure | URL-only classifier + sweep + Acknowledge UI | Active reachability probe; enforcement (block public flows) | #14 |
| UC9 — Credential governance | Caller registry + NEW-flag + signature dedup | IAM integration (Okta/Entra); enforcement (block unapproved creds) | #15 |
| UC10 — Distributed enforcement | First slice (shim CEL) | Real agentgateway integration; bundle signing; canary rollout; endpoint-mode agent | #13 |

## Roadmap items the code already hints at

A few load-bearing roadmap items have placeholders in the data model:

- `policy_bundle_version` on `orgs` (migration `00014`) plus the bump trigger plus the heartbeat header — distributed enforcement v1 is wired top-to-bottom and the second-slice work is "swap shim for real proxy", not "design the protocol".
- `mcp_connections.auth_method` includes `oauth2_authcode`, and DCR (RFC 7591) is fully implemented (`cloud/api/internal/mcp/dcr.go`) — so DCR-based MCP onboarding is already enterprise-grade.
- `org_daily_metrics` (migration `00015`, **in flight** as of this writing) is the substrate for the executive dashboard. The migration exists; the rollup pipeline + OverviewPage rewrite are landing in the current wave by a parallel agent.
- `org_invitations` + the invitee-facing routes (`/api/me/invitations`, `/accept`, `/decline`) — email-driven invites are shipping.

## Roadmap items the code does **not** hint at

If you're scoping work, these are greenfield:

- DNS-passive discovery. There is no DNS telemetry consumer, no Infoblox DDI integration, no model for DNS-derived `mcp_servers` rows. The operating-plan explicitly calls this out as the largest vision gap.
- Network-aware policy (UC7). The CEL env has no `network` or `vpc` variable; adding one is non-trivial because it has to be supplied by the data plane and today's data plane (the shim) has no network-context concept.
- Real agentgateway integration (UC10 second slice). The shim today is not the production data plane.
- Multi-region. Single-region us-central1. No data-residency story.
- Customer roles beyond `owner|admin|member`. No support-engineer or auditor tier.
- SOC 2 Type II. Prep is on the operating-plan; not in audit today.

## Bus factor

Operating-plan §06 names this directly: six waves shipped, all by one lead engineer with parallel AI agents. The first incremental hire is the Platform/Product Engineer to become a second wave-driver. The wave model is documented in the project root `CLAUDE.md`. For an architect: this is a continuity risk that depends on the second-hire timing more than on any single technical choice.

## Cross-reference

- [system overview](./01-system-overview.md) — what runs where.
- [policy and enforcement](./06-policy-and-enforcement.md) — the contract that today's shim and tomorrow's agentgateway both implement.
- [trust boundaries](./11-trust-boundaries.md) — the threat-surface gaps (suspension not enforced, bundle unsigned, no per-org rate limit) that pair with the roadmap above.
