# BRIGHT GUARD · OPERATING PLAN

**The AI-Augmented Team That Turned a Vision Into a Shipping Service**

Phase 2 visibility and the Phase 3 governance core are already shipping on
`mcp-governance.infoblox.dev`. This plan covers the incremental investment
required to (a) close the remaining vision use cases, (b) ship the enforcement
second-slice, and (c) harden the product for enterprise sale.

---

## 01 — Starting Point

**Already built · shipping in production · ~$0 capex to date**

Six development waves over ~6 elapsed weeks have delivered the foundation,
built by **one lead engineer + parallel AI-agent workers in isolated git
worktrees**. The wave model is documented in `CLAUDE.md` and has been proven
at a cadence of 2–5 GH issues per wave, 3–4 parallel agents, ~1–2 days per
wave end-to-end.

### What is shipping today

| Layer | What's live |
|---|---|
| Visibility (UC1–3) | Gateway-based MCP server discovery, capability catalog from observation ingest, append-only audit trail with org-wide activity timeline + filters + cursor pagination |
| Governance authoring (UC4) | CEL policy primitive, PoliciesPage + PolicyDetailPage, audit-only decisions |
| Detection (UC8/9) | Exposure-state classifier + sweep (RFC1918, cloud-internal, public), per-org caller registry with canonical-signature dedup, NEW-flag rollover, Acknowledge flow |
| Distributed enforcement (UC10 first slice) | Heartbeat carries org policy bundle; shim embeds cel-go, evaluates locally, returns decisions; observations carry decision state |
| Platform | Cloud Run + Cloud SQL Postgres, multi-tenant org model, Google OAuth, OAuth2 device-code for CLI, OAuth2 DCR for MCP server enrollment, platform-admin console, demo shim heartbeating 24/7 against prod |

### Use case coverage

| Status | Use Cases |
|---|---|
| Core shipped | UC1 MCP Server Discovery, UC2 Capability Discovery, UC3 Monitoring & Audit Trail |
| Partial — authoring or detection only | UC4 Policy Definition, UC8 Internal/External Exposure, UC9 Credential Governance, UC10 Distributed Enforcement |
| Not started | UC5 Simulation, UC6 Asset & User Policy, UC7 Network-Aware Enforcement, UC11 Unified Policy |

**Standing infrastructure cost: ~$25/month** (Cloud SQL + Cloud Run service +
demo shim with `min-instances=1`). Engineering capex to date is bounded by AI
API spend and the lead engineer's time.

### What "incremental investment" means here

This is not "build the governance layer" — that layer is shipping. The
incremental investment covers four areas:

1. **Enforcement second-slice** — wire CEL → shim-deny for UC8/9; ship real
   agentgateway integration for UC10.
2. **The unshipped UCs** — UC5 simulation, UC6 asset/user policy, UC7
   network-aware enforcement, UC11 unified policy console.
3. **The network-native vision pillar** — DNS-passive discovery, currently
   unshipped, which the vision describes as the *primary* signal.
4. **Enterprise hardening** — SOC 2 Type II, pen test, SIEM/OTel export,
   retention/partitioning, customer-facing roles.

---

## 02 — Team — Incremental Investment

The development model demonstrated through six waves is **one lead engineer +
N parallel AI agents per wave**, isolated by git worktrees, merged manually,
deployed via a single `deploy.sh` to Cloud Run. That model carries forward.
The incremental hires below target productization, security architecture,
customer-facing roles, and compliance — not raw engineering throughput.

### Engineering · New

| Role | Headcount | Rationale |
|---|---|---|
| Platform / Product Engineer | 1 | Co-owner with the lead engineer. Drives waves in parallel, owns architectural decisions jointly. **First-priority hire** — removes the bus-factor-of-one risk that the current model carries. Senior level. |
| Backend Engineer — Enterprise Integrations | 1 | SIEM/SOAR + OTel export, retention/partitioning of `mcp_invocations`, IAM integration (Okta/Entra) for UC9 enforcement, agentgateway subprocess scrape for UC10 second-slice. Mid-senior. |

### Security · New

| Role | Headcount | Rationale |
|---|---|---|
| Security Architect | 1 | **Most critical hire.** Owns the policy model evolution beyond the CEL primitive, simulation logic (UC5), network-aware enforcement design (UC7), and the threat-model narrative that anchors the GTM story. Blocks Phase 4 if unfilled. Principal / staff level. If redeployed internally from Infoblox security, this is $0 incremental. |

### Product · New

| Role | Headcount | Rationale |
|---|---|---|
| Product Manager | 1 | Owns the governance roadmap, design partners, use-case prioritization. Enterprise security product experience required. Internal transfer possible. |
| Product Designer | 0.5–1 | Policy UI, simulation experience, dashboards. Part-time through Phase 1, full-time from Phase 2. Effective load ~0.6 FTE in Year 1. |

### Customer-Facing · New

| Role | Headcount | Rationale |
|---|---|---|
| Solutions Engineer / Customer Success | 1 | Drives design-partner pilots, onboards customers onto the gateway model, owns the gateway-integration partner motion (Runlayer, MintMCP). Required from Day 30. |

### Redeployed / Shared

| Role | Effort | Rationale |
|---|---|---|
| Existing AE team (installed base) | No new hires | GTM motion is installed-base first. Investment is enablement — Solution Prompter, MCP Inspect tool, use-case brief — not headcount. |
| Legal / Security Review | Shared | SOC 2 Type II preparation begins Phase 1. Leverage Infoblox compliance infrastructure. |
| Infoblox DDI / Network Telemetry team | ~10% × 90 days | One-time scoping for the DNS-passive slice of UC1 — the network-native vision pillar that has not yet shipped. Cross-team contract required before Phase 2 ends. |

**Total incremental headcount: 5 FTE + 0.5 designer.** Lower than a
conventional governance-from-scratch build because the foundation is in code
and the wave-based AI-augmented dev model has been proven through six shipped
waves.

---

## 03 — Build Costs — Year 1

*Incremental investment only. Foundation engineering complete at ~$0 capex.
Costs assume US-based team; hybrid offshore reduces engineering line ~40–60%.*

| Area | Assumptions | Est. Cost (Y1) |
|---|---|---|
| Foundation engineering (already shipped) | Six waves by one lead engineer with parallel AI agents over ~6 weeks. AI API spend + ~$25/mo Cloud infra. | **$0 incremental** |
| Platform / Product Engineer | $200K fully-loaded. | $200K |
| Backend Engineer — Enterprise Integrations | $180K fully-loaded. | $180K |
| Security Architect | Principal / staff. $220–250K fully-loaded. Critical-path. Internal transfer drops this to ~$0. | $240K |
| Product Manager | $180K fully-loaded. Internal transfer possible. | $180K |
| Product Designer | $120K fully-loaded × ~0.6 FTE effective. | $75K |
| Solutions Engineer / CS | $160K fully-loaded. From Day 30. | $160K |
| AI agent compute (ongoing) | Continued AI-augmented wave development. ~1–2 concurrent agents × ~50 waves/year. | $40K |
| Infrastructure & Cloud | Multi-tenant Postgres + Cloud Run + observation ingest at scale. Audit retention 3yr. Year 1 at 5–10 production customers. | $120K |
| SOC 2 Type II preparation | Third-party auditor + internal prep. 6–9 month process. Start Phase 1. | $60K |
| Third-party security assessment | Pre-GA pen test, before enterprise launch. | $40K |
| Sales enablement | Solution Prompter, MCP Inspect tool, use-case briefs, presales support. | $80K |
| Launch & Marketing | RSA / Black Hat presence, "State of Shadow MCP" report, content. | $150K |
| Partner integration engineering | 2 gateway integrations (Runlayer, MintMCP). ~3–4 weeks each + maintenance. | $60K |
| **Total Incremental — Year 1** | Excludes foundation engineering. US-based team. | **~$1.59M** |

### Levers

- **Security Architect + PM as internal transfers** → drop ~$420K → **~$1.17M**
- **Hybrid US/offshore engineering** (50/50 on Platform Eng + Backend Eng) → drop ~$170K → **~$1.42M** (combined with the transfer lever: **~$1.0M**)
- **Breakeven: 9–12 paying customers at $100–150K ACV** depending on token consumption.

---

## 04 — Revenue Journey — Four Quarters

**First revenue — Day 45 · First paid pilot signed via existing AE relationship**
**Q1 exit — $750K · 5 paid pilots committed via token model**
**Day 90 pipeline — $5M qualified pipeline per GTM exit metric**
**Year 1 ARR — $2M · ~15–20 paying customers**

The Day 0 starting position is genuinely different from the conventional plan:
the prototype is the production service. Walkthroughs run against a live
multi-tenant control plane with a demo shim emitting realistic traffic 24/7.

### Phase 01 · Days 1–30 · Activate

- **Live-product walkthroughs** (not slideware) with 3–5 design-partner CISOs from Infoblox installed-base accounts
- Free-trial campaign across 200 targeted installed-base accounts using the deployed product
- Sales enablement: Solution Prompter + MCP Inspect tool + use-case brief deployed to field
- SOC 2 Type II preparation initiated
- **Exit metric:** 200 accounts activated

### Phase 02 · Days 31–60 · Discover & Qualify

- Solution Prompter calls with the full buying committee (CISO + CAO + Risk Officer)
- MCP Inspect run against qualified accounts → turns discovery call into a live report
- First 5 paid pilots signed — tied to existing renewal or uDDI deal, single PO via token model
- Design partners deploy gateways into staging environments
- **Exit metric:** 5 pilots · $750K committed

### Phase 03 · Days 61–90 · Validate

- First 3 paid customers running **live policy enforcement** — requires Wave N+6 (UC8/9 enforcement second-slice) to ship in this window
- Publish "State of Shadow MCP" report — anchors press story, drives inbound
- Sales play codified into Q+1 quota plans across the installed-base AE team
- **Exit metric:** $5M qualified pipeline

### Phase 04 · Days 91–365 · Scale

- GA launch at RSA or Black Hat — lead with threat narrative, not feature list
- Governance tier live: network-aware policy (UC7), DNS-passive enforcement (UC1 vision pillar), SIEM integration
- Reporting tier live: pre-mortem simulation (UC5), audit-ready exports
- 2 gateway partner integrations (Runlayer, MintMCP) live as enforcement outputs
- MSSP channel activated — BloxAgent as add-on to managed DDI contracts
- **Exit metric:** 20 customers · $2M ARR

---

## 05 — Revenue Model & Unit Economics

Three tiers matching the GTM pricing structure. The token model removes
new-procurement friction — Bright Guard consumes from the pool customers
already commit to.

### ACV by Tier

| Tier | Price | Y1 Target Mix |
|---|---|---|
| Visibility | Free — included with DDI + TD | Land wedge |
| Govern | Token-metered · est. $50–100K ACV | 12 customers |
| Reporting + Simulation | Token-metered · est. $100–150K ACV | 8 customers |
| **Blended ARR** | **~$100K blended ACV across 20 customers** | **~$2M** |

### Use Case Coverage by Phase (revised — reflects what is already in code)

| UC | Status today | Phase 2 (Days 90–180) | Phase 3 (Days 180–365) |
|---|---|---|---|
| UC1 MCP Server Discovery | Shipped (gateway-based) | + DNS-passive slice; fleet view | + cross-gateway server merge |
| UC2 Capability Discovery | Shipped (via observations) | + live introspection; drift alerts | + capability tagging (read/write/destructive) |
| UC3 Monitoring & Audit | Shipped (UI + API) | + SIEM/OTel export; retention/partitioning | + anomaly alerts |
| UC4 Policy Definition | Authoring shipped (CEL audit-only) | + policy versioning + revert | + decision-audit linkage |
| UC5 Pre/Post Simulation | Not started | scope + design | ship |
| UC6 Asset & User Policy | Not started | CEL env audit + scope | ship |
| UC7 Network-Aware Enforcement | Not started | — | ship |
| UC8 Internal/External Exposure | Detection shipped | + active reachability probe | + enforcement (block public flows) |
| UC9 Credential / Key Governance | Detection shipped | + IAM integration (Okta/Entra) | + enforcement (block unapproved creds) |
| UC10 Distributed Enforcement | First slice shipped (shim + CEL) | + real agentgateway; bundle signing | + canary rollout; endpoint-mode agent |
| UC11 Unified Policy Console | Not started | — | unified across gateway + endpoint |

---

## 06 — Risk Register

Eight material risks across build, market, and GTM. The single biggest
reframe versus a generic plan: **the network-native vision pillar is not yet
in code.** What is shipping today is gateway-based, which changes which risks
are material.

| Cat | Risk | Mitigation | Severity |
|---|---|---|---|
| Build · Vision gap | **Gateway-based discovery does not fulfill the network-native pillar.** Vision text describes DNS as the primary signal. Today's discovery requires the customer to deploy a gateway. Overstating coverage in sales conversations damages credibility. | File the DNS-passive slice of UC1 as a concrete Phase 2 issue with a named owner on the Infoblox DDI team. Sales framing: "gateway-first, DNS-augmented" until DNS-passive ships. | **High** |
| Build · Bus factor | **AI-augmented wave development depends on one lead engineer.** Six waves shipped, all by the same human. Continuity risk if the lead is unavailable. | First incremental hire is the Platform/Product Engineer who becomes a second wave-driver. Wave model documented in CLAUDE.md (done). | **High** |
| Build · Enforcement gap | UC8/9 enforcement (the second-slice that turns *detection* into a *block*) must ship before paid pilots can credibly market "governance" rather than "visibility." Phase 3 timeline assumes Wave N+6 ships this. | Wave N+6 already scoped: CEL → shim-deny for exposure + caller, three parallel agents, disjoint footprints. | **High** |
| GTM · Buyer | Infoblox AE relationships are with network admins and infrastructure teams. The MCP governance buyer is the CISO and CAO. Existing AEs may not be selling there today. | Co-sell motion: network champion pulls CISO in using MCP Inspect report. Sales enablement to navigate the security buyer. Token model removes the new-PO friction. | **High** |
| Compliance · Enterprise | SOC 2 Type II gap blocks finance and healthcare buyers. 6–9 months to prep + audit. | Start preparation in Phase 1. Leverage Infoblox compliance infrastructure. Target completion by GA (Day 180). | **High** |
| Build · Capability | Enterprise MCP servers often require auth before exposing tool lists; automated `tools/list` enumeration may be rejected. | Manual capability declaration as fallback. Today we already infer capabilities from observation ingest, which doesn't require an extra crawl. | Medium |
| Competitive | Gateway players (Runlayer, MintMCP) add discovery features. They could partner with a DNS provider and replicate the moat. | Speed via installed base is the defense. The cross-tenant DNS baseline + threat intelligence is the durable moat — *once DNS-passive ships*. Move fast on design partners. | Medium |
| GTM · Market timing | Design partners may not have MCP in production yet. Market is real but Infoblox installed base may lag. | Prototype walkthroughs against the live demo shim still validate use cases. The "shadow MCP" threat resonates hypothetically. Prioritize partners with publicly announced AI agent programs. | Medium |
| Protocol · Standards | MCP replaced or fragmented by A2A / ACP / UCP within 18 months. | Issue #37 (A2A epic) already filed. Architecture is protocol-agnostic. Long-term positioning is "AI agent infrastructure governance," not "MCP governance." | Low |

---

## 07 — What Has To Be True

Five assumptions underpin the entire plan. If any is false, the operating
model needs revision before Phase 1 starts.

### Assumption 01 · DNS-passive discovery is reachable from existing DDI telemetry

The vision's network-native positioning assumes BloxOne DNS telemetry can be
extended to detect MCP traffic patterns without a full pipeline rebuild.
**This is the biggest open assumption** — today's product uses zero DNS
telemetry. If reaching that signal requires a net-new pipeline, Phase 2
slips 60–90 days and the vision-positioning gap widens.

### Assumption 02 · Wave-based AI-augmented development scales beyond solo

Six waves shipped over ~6 weeks proves the cadence at one-lead-plus-AI scale.
The plan assumes that cadence continues with a small incremental team. If
the wave model breaks under two or more humans coordinating concurrent
worktrees, the demonstrated productivity gains evaporate and engineering
costs scale up toward conventional levels.

### Assumption 03 · Token model removes procurement friction

Customers can activate Govern and Reporting tiers by consuming from existing
Infoblox token pools — no new PO, no new procurement cycle. If the token
model isn't live or doesn't cover Bright Guard, the 90-day revenue targets
need to be revised down.

### Assumption 04 · Existing AEs can carry the CISO motion

The installed-base GTM works only if existing AEs can navigate a CISO
conversation, not just a network admin conversation. Requires meaningful
enablement and possibly presales engineering support — not just a sell sheet.

### Assumption 05 · Gateway players will partner, not compete

Enforcement-output integrations with Runlayer and MintMCP assume they see
Bright Guard as complementary. If they build their own network-layer
discovery, the partnership story breaks. Validate partnership intent in
Phase 1 — before engineering resources are committed to the integrations.

---

*BRIGHT GUARD · OPERATING PLAN · CONFIDENTIAL*
