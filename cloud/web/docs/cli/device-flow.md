# CLI: the device-code flow

Bright Guard exposes the same REST API the SPA uses for any scripts, CI
jobs, or local tools you want to drive it from. CLI auth happens via the
**OAuth 2.0 Device Authorization Grant** (RFC 8628) — you start an
authorization on the CLI, approve it in a browser, and the CLI gets a
bearer token tied to the same user identity as your SPA session.

The reference implementation ships as a shell script at `cloud/cli/bg-auth.sh`.

## Quick start

```sh
# Default control plane, default label.
./bg-auth.sh

# CI-friendly label so you can find the session later in Settings → Sessions.
BG_CLIENT_LABEL="ci-deploy-runner" ./bg-auth.sh

# Point at a non-production control plane.
BG_CONTROL_PLANE=http://localhost:8080 ./bg-auth.sh
```

The script writes the token to `~/.config/brightguard/credentials` (mode
`0600`) and prints the curl recipe to use it.

## What the flow looks like under the hood

`bg-auth.sh` is a thin wrapper over three HTTP endpoints. If you can't run
the shell script (locked-down environment, different shell, fancy CI runner),
you can drive the flow yourself.

### 1. Initiate

```sh
curl -sS -X POST "https://mcp-governance.infoblox.dev/oauth/device" \
  -H 'Content-Type: application/json' \
  -d '{"clientLabel":"my-laptop"}'
```

Response:

```json
{
  "deviceCode": "…opaque, keep this secret…",
  "userCode": "ABCD-EFGH",
  "verificationUri": "https://mcp-governance.infoblox.dev/device",
  "verificationUriComplete": "https://mcp-governance.infoblox.dev/device?code=ABCD-EFGH",
  "interval": 5,
  "expiresIn": 600
}
```

- `deviceCode` is the long-lived secret you'll poll with. Treat it like a
  password.
- `userCode` is the short string the user types in if they can't follow the
  `verificationUriComplete` link.
- `interval` is the floor on poll frequency — don't go faster than this.
- `expiresIn` is seconds; ten minutes by default.

### 2. User approves

Either open `verificationUriComplete` in a browser (already includes the
code), or visit `verificationUri` and type the `userCode`. The page calls
`POST /api/device/approve` against the user's session cookie.

If the user is signed in as a member of multiple orgs, the page surfaces a
picker — the resulting bearer token is scoped to the org they chose.

### 3. Poll for the token

```sh
curl -sS -X POST "https://mcp-governance.infoblox.dev/oauth/device/poll" \
  -H 'Content-Type: application/json' \
  -d '{"deviceCode":"…from step 1…"}'
```

Possible responses:

| HTTP | Body | Meaning |
|---|---|---|
| `200` | `{"accessToken":"bg_cli_…"}` | Approved. The body is your bearer token. |
| `428` | `{"error":{"code":"authorization_pending"}}` | Still waiting for the user. Sleep `interval` seconds and try again. |
| `403` | `{"error":{"code":"denied"}}` | User clicked **Deny**. Start over. |
| `410` | `{"error":{"code":"expired"}}` | `expiresIn` elapsed without approval. Start over. |

## Using the token

```sh
export BG_TOKEN=$(cat ~/.config/brightguard/credentials)

# Who am I, and which orgs?
curl -H "Authorization: Bearer $BG_TOKEN" \
  https://mcp-governance.infoblox.dev/api/me

# List gateways in an org from /api/me.
ORG_ID=...
curl -H "Authorization: Bearer $BG_TOKEN" \
  https://mcp-governance.infoblox.dev/api/orgs/$ORG_ID/gateways
```

The token's permissions exactly mirror the user's session — anything you
can do in the SPA, the bearer can do too.

## Token format

CLI tokens look like:

```
bg_cli_<session-uuid>.<32-byte-base64url-secret>
```

The control plane stores only the HMAC of the secret half. Lose the token
and you can't recover it; revoke it from **Settings → Sessions**.

## Revoking

Each CLI session shows up as a row on `/app/settings/sessions` with the
label you passed in `BG_CLIENT_LABEL`. Click **Revoke** to invalidate the
token server-side immediately. You can't revoke the cookie session you're
currently using from the same page; sign out instead.

## Headed approval for agents

For automated agents driving the control plane (e.g. the QA agent from the
project README), the typical pattern is:

1. Agent calls `/oauth/device` and surfaces the `userCode` to a human.
2. Human approves at `/device?code=<userCode>`.
3. Agent polls and proceeds with the resulting token.

This avoids the dev-login backdoor in production while still letting
headless workflows authenticate.
