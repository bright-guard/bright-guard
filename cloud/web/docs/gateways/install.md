# Installing a gateway

A **gateway** (also called the **shim**) is a small Go binary that sits next
to your MCP servers and brokers their traffic. It does three things:

1. Sends a **heartbeat** to the control plane every 30 seconds. The response
   carries the current policy bundle and the set of disabled capabilities.
2. Streams **observations** — every MCP invocation it sees, with caller
   identity, capability name, latency, and status.
3. **Enforces** the policy bundle locally: it evaluates CEL expressions
   in-process and flips invocations to `denied` when a `deny`-action policy
   matches. The control plane re-evaluates server-side too so an offline
   shim can't be tricked into letting denied traffic through.

The shim's source is at `cloud/shim/`; the prebuilt image is published as
`us-central1-docker.pkg.dev/bright-guard-prod/bright-guard/bright-guard-shim:latest`.

## Step 1 — Create the gateway in the SPA

You can't enroll a gateway from the host until the control plane knows about
it. From the tenant SPA:

1. Navigate to **Gateways** in the left rail (`/app/gateways`).
2. Click **Add gateway**. The org selector at the top must show the right org
   first — Bright Guard scopes gateways to a single org and there's no
   "move gateway" flow.
3. Give it a name (e.g. `prod-us-east-1`). Click **Create**.
4. Copy the **enrollment token** the modal shows you. It's a one-shot
   credential — the shim trades it for a long-lived gateway credential on
   first heartbeat. If you lose it before the shim runs, click **Reissue
   enrollment token** on the gateway detail page.

The enrollment token is committed (consumed) only when the shim's first
heartbeat succeeds. A shim that crashes mid-enrollment leaves the token
re-usable, so you don't have to reissue on every transient failure.

## Step 2 — Configure the host

The shim needs three things in its environment:

| Variable | Required | Purpose |
|---|---|---|
| `BG_CONTROL_PLANE` | yes | Base URL of the control plane, e.g. `https://mcp-governance.infoblox.dev`. |
| `BG_ENROLLMENT_TOKEN` | first run only | Token from step 1. Drop it after the credential has been persisted. |
| `BG_CREDENTIAL` | after enrollment | Long-lived credential the shim writes to `BG_STATE_DIR/credential` and reads on startup. Pre-populate this in stateless environments. |
| `BG_CONFIG` | yes | Path to a YAML file declaring the servers the shim brokers. Defaults to `/etc/brightguard/shim.yaml`. |
| `BG_STATE_DIR` | no | Directory the shim uses to persist the credential. Defaults to `/data`. |
| `PORT` | optional | If set, the shim listens on `/healthz` so it can run as a Cloud Run service. |

A minimal `shim.yaml` for a single MCP server looks like this:

```yaml
servers:
  - name: jira-prod
    address: https://jira.example.com/mcp
    transport: streamable-http
    version: "1.0.0"
    capabilities:
      - kind: tool
        name: search_issues
        description: Search Jira issues by JQL.
      - kind: tool
        name: create_issue
        description: Create a Jira issue.
```

There's a realistic fake-server config the demo shim uses at
`cloud/shim/examples/fake-servers.yaml`; copy and adapt it if you need a
multi-server starting point.

## Step 3 — Run the shim

### Docker (recommended)

```sh
docker run --rm \
  -e BG_CONTROL_PLANE=https://mcp-governance.infoblox.dev \
  -e BG_ENROLLMENT_TOKEN=<paste from SPA> \
  -e BG_CONFIG=/etc/brightguard/shim.yaml \
  -v $(pwd)/shim.yaml:/etc/brightguard/shim.yaml:ro \
  -v bg-state:/data \
  us-central1-docker.pkg.dev/bright-guard-prod/bright-guard/bright-guard-shim:latest
```

### From source

```sh
cd cloud/shim
go build -o bg-shim ./cmd/shim
BG_CONTROL_PLANE=https://mcp-governance.infoblox.dev \
BG_ENROLLMENT_TOKEN=<paste from SPA> \
BG_CONFIG=./examples/fake-servers.yaml \
BG_STATE_DIR=./state \
./bg-shim
```

### Cloud Run

The same image runs as a Cloud Run service if you set `PORT`. The demo
shim (`bright-guard-shim-demo`) does exactly this with `min-instances=1`
and a credential pre-populated from Secret Manager:

```sh
gcloud run deploy bright-guard-shim-demo \
  --image=us-central1-docker.pkg.dev/bright-guard-prod/bright-guard/bright-guard-shim:latest \
  --region=us-central1 \
  --min-instances=1 \
  --set-secrets=BG_CREDENTIAL=bg-shim-bg-credential:latest \
  --update-env-vars=BG_CONTROL_PLANE=https://mcp-governance.infoblox.dev,BG_CONFIG=/app/shim.yaml
```

## What you should see

Within 30 seconds of the shim starting, refresh the SPA:

- The gateway row on **Gateways** flips from `pending` to `healthy`. The
  **Last seen** timestamp updates each heartbeat.
- **MCP Servers** lists every server the shim declared in `shim.yaml`, with
  their capabilities expanded.
- **Activity** starts showing invocations. The demo shim emits realistic fake
  ones every 30s; a real shim only emits when traffic flows through it.

If you don't see the heartbeat within a minute, check the shim's stdout —
401 errors mean the enrollment token was rejected (revoked, reused, or
mistyped); 5xx errors usually mean a CSP/network egress issue between the
host and `mcp-governance.infoblox.dev`.

## Removing a gateway

From the SPA: **Gateways → (row) → Delete**. The credential is invalidated
server-side immediately; the next heartbeat will fail and the shim will exit
non-zero. Restart it with a fresh enrollment token if you want it back.
