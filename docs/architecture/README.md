# Architecture · Bright Guard

Bright Guard is a multi-tenant SaaS control plane for MCP (Model Context Protocol) servers. A single Go binary (Cloud Run) terminates browser, CLI, and gateway-shim traffic, persists state to Postgres (Cloud SQL), and serves an embedded React SPA. Customer-deployed *shims* push MCP server inventory, capability lists, and invocation observations into the control plane; the control plane pushes back a CEL policy bundle that the shim evaluates locally. Today's deployment is `mcp-governance.infoblox.dev`.

This directory is the engineering reference for the system as it ships today. Every claim cites a path inside `cloud/`. Roadmap items are explicitly labeled — see [12-tradeoffs-and-roadmap.md](./12-tradeoffs-and-roadmap.md).

## Audience

- **Internal engineers / new hires** — onboarding, request flows, gotchas, file footprints.
- **Enterprise architects evaluating Bright Guard** — component topology, trust boundaries, data model, deployment.

Where the two diverge (e.g. "gotcha" callouts) it is labeled.

## Index

| File | Topic |
|---|---|
| [01-system-overview.md](./01-system-overview.md) | 30,000-foot view + component diagram |
| [02-repository-layout.md](./02-repository-layout.md) | Directory map of `cloud/` |
| [03-data-model.md](./03-data-model.md) | Postgres schema + ERD |
| [04-request-flows.md](./04-request-flows.md) | Sequence diagrams for browser, gateway, heartbeat, device-code |
| [05-authentication.md](./05-authentication.md) | Google OIDC, sessions, device flow, DCR, gateway bearer, platform admin |
| [06-policy-and-enforcement.md](./06-policy-and-enforcement.md) | CEL engine, policy bundle, shim local eval, server sweep |
| [07-background-processes.md](./07-background-processes.md) | Schedulers + advisory locks |
| [08-deployment.md](./08-deployment.md) | `deploy.sh`, image build, env vars, migrations |
| [09-multi-tenancy.md](./09-multi-tenancy.md) | Org model, RBAC, isolation boundary |
| [10-observability.md](./10-observability.md) | What's in (healthz, logs); what's missing (OTel, traces) |
| [11-trust-boundaries.md](./11-trust-boundaries.md) | TLS, secrets, threat surfaces |
| [12-tradeoffs-and-roadmap.md](./12-tradeoffs-and-roadmap.md) | Vision-vs-reality gaps; gateway-now vs DNS-passive-later |

## Conventions

- Source citations look like `cloud/api/internal/api/server.go:188` and resolve from the repo root.
- Mermaid blocks render natively on GitHub. No runtime mermaid dependency.
- Roadmap items are flagged "not yet built" in-line.
