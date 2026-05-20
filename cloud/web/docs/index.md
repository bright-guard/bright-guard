# Bright Guard documentation

Bright Guard is the control plane for AI-infrastructure security: it discovers
the MCP servers your agents touch, governs which capabilities they can call,
and enforces policies at the gateway.

These docs cover everything you need to install, configure, and operate
Bright Guard. They're aimed at platform engineers and SREs running it for
their company — not first-time MCP users.

## Where to start

- New to Bright Guard? [Getting started](getting-started.md) walks through
  sign-in, creating an org, and inviting a teammate in about five minutes.
- Already signed in? Drop a [gateway](gateways/install.md) onto a host that
  speaks to an MCP server, then [add a connection](connections/adding-an-mcp-connection.md)
  to point at one of the MCP services you want to govern.
- Writing your first rule? Read the [CEL primer](policies/cel-primer.md).

## How the pieces fit together

A Bright Guard deployment has three planes:

1. **Control plane** — the `bright-guard` Cloud Run service at
   `mcp-governance.infoblox.dev`. Hosts the SPA, the REST API, the policy
   compiler, and the activity store. This is what you're looking at right now.
2. **Gateways** — small shims you install next to your MCP servers. They
   stream observations of every invocation to the control plane and enforce
   the policy bundle they receive on each heartbeat. See
   [Installing a gateway](gateways/install.md).
3. **Connections** — outbound credentials the control plane uses to discover
   capability schemas from each MCP server (and to drive remote MCP servers
   directly). Static API keys, bearer tokens, basic auth, and OAuth2 with
   either pre-registered or dynamic clients are supported. See
   [Adding an MCP connection](connections/adding-an-mcp-connection.md).

## Reference

- [HTTP route table](reference/routes.md)
- [CEL environment](reference/cel-env.md)
- [API error codes](reference/error-codes.md)

The three pages above are generated from source by `cloud/api/cmd/gen-docs`.
If something in production looks off but the table says otherwise, the table
is stale — re-run the generator.
