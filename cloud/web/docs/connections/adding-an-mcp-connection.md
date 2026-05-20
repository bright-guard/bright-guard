# Adding an MCP connection

An **MCP connection** is the credential Bright Guard uses to talk to a remote
MCP server. The control plane uses it for two things:

1. **Discovery** — pulling the list of tools, resources, and prompts the
   server advertises, so the SPA can show capability inventories and policies
   can reference them by name.
2. **Direct invocation** (future) — driving the MCP server on behalf of
   tenants in a "Bright Guard as proxy" mode.

Connections are different from gateways: a gateway lives next to your MCP
server and brokers traffic; a connection lives in the control plane and lets
Bright Guard reach out to one. You can have both for the same server, or
just one.

## Open the wizard

From the SPA: **MCP Connections** in the left rail
(`/app/mcp-connections`) → **Add connection**. A three-step modal appears:

1. **Endpoint** — name, URL, and transport.
2. **Auth** — which credential to use.
3. **Confirm** — review and create.

## Step 1 — Endpoint

| Field | Notes |
|---|---|
| Name | Free-form label shown throughout the SPA. Must be unique within the org. |
| Endpoint URL | Absolute `http://` or `https://` URL of the MCP server. |
| Transport | `streamable-http` is the default. `stdio` is not supported for remote connections. |

## Step 2 — Auth

The wizard offers four auth methods. The structured `dcr_unsupported` error
code (422) is returned if you pick auto-DCR and the server doesn't advertise
the metadata required — the wizard handles this by flipping you to manual
mode automatically; you'll see the explanation banner appear.

### API key header

For services that take an API key in a custom header.

| Field | Example |
|---|---|
| Header name | `X-Api-Key` |
| Header value | `sk_live_abc123…` |

The value is encrypted at rest and never re-displayed in the UI.

### Bearer token

Plain `Authorization: Bearer <token>`.

### Basic auth

Standard HTTP basic. Stored as a username and password pair.

### OAuth2 (authorization code)

The richest option, and the one most production deployments use. Bright
Guard manages the access-token / refresh-token lifecycle itself; you only
provide the application credentials.

#### Auto-register (Dynamic Client Registration)

If the upstream advertises RFC 7591 Dynamic Client Registration, pick
**Auto-register**. Bright Guard will:

1. Fetch the OAuth metadata document.
2. POST a `client_metadata` document and store the issued
   `client_id` / `client_secret`.
3. Redirect your browser to the provider's `/authorize` endpoint with
   `prompt=consent` so the consent screen never gets cached past a token
   refresh.
4. Exchange the resulting auth code for tokens and store the encrypted
   bundle.

If the server returns a 422 with `code=dcr_unsupported`, the wizard flips you
into **Manual mode** with the explanation banner.

#### Manual mode

You provide the OAuth metadata yourself. There are presets for the most
common providers:

- **Atlassian Cloud (Jira / Confluence)** — pre-fills `auth.atlassian.com`
  endpoints and the `audience` / `prompt` extras.
- **Notion** — pre-fills `api.notion.com` endpoints and the `owner=user`
  extra.

| Field | Notes |
|---|---|
| Authorize URL | `https://provider.example.com/oauth2/authorize` |
| Token URL | `https://provider.example.com/oauth2/token` |
| Client ID | From the provider's developer console. |
| Client secret | Treated as sensitive, encrypted at rest. |
| Scopes | Space-separated. |
| Extra params | A JSON object merged into the `/authorize` query — e.g. `{"audience":"api.atlassian.com","prompt":"consent"}`. |

## Step 3 — Confirm and authorize

Click **Create**. For non-OAuth methods, the connection is created and
discovery starts immediately — within a few seconds the **MCP Servers** page
will show the capabilities the upstream returned.

For OAuth2 you'll be redirected to the provider's consent screen. After
approval, the browser comes back to `/oauth/connect/callback` on the
Bright Guard control plane, which exchanges the auth code, stores the token
bundle, and bounces you back into the SPA.

## Re-running discovery

If a server adds new tools, you can re-trigger discovery without recreating
the connection: open the connection detail page and click **Re-discover**.
This calls `POST /api/orgs/{orgId}/mcp-connections/{id}/discover` under the
hood.

## Common errors

| Error | What to do |
|---|---|
| `dcr_unsupported` (422) | Server doesn't support automatic registration. Wizard auto-flips to manual mode. |
| `invalid_request` (400) on `oauthConfig is required` | You picked OAuth2 manual mode and one of the URL/client fields is empty. |
| Stuck on "discovering…" | Check the upstream is reachable from the control plane and the credential is valid. The wizard's status bar surfaces the underlying HTTP code. |

See the [API error codes reference](../reference/error-codes.md) for the full
list.
