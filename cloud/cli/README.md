# bg-auth

Device-code authorization helper for the Bright Guard control plane.

`bg-auth.sh` initiates a device authorization, prompts you to approve it in a
browser, then writes the resulting bearer token to
`~/.config/brightguard/credentials` (mode 0600).

## Usage

```sh
# Default control plane and label.
./bg-auth.sh

# Custom label (shown on the approval screen and in Settings → Sessions).
BG_CLIENT_LABEL="ci-runner" ./bg-auth.sh

# Point at a different control plane (e.g. local dev).
BG_CONTROL_PLANE=http://localhost:8080 ./bg-auth.sh
```

## Using the token

Once `bg-auth.sh` succeeds, hit any cookie-authenticated API endpoint with the
bearer token.

```sh
export BG_TOKEN=$(cat ~/.config/brightguard/credentials)

# Who am I?
curl -H "Authorization: Bearer $BG_TOKEN" https://mcp-governance.infoblox.dev/api/me

# Pick an org from /api/me and list its gateways:
ORG_ID=...
curl -H "Authorization: Bearer $BG_TOKEN" \
  https://mcp-governance.infoblox.dev/api/orgs/$ORG_ID/gateways
```

## Revoking access

CLI sessions appear in the SPA under **Settings → Sessions**. Click **Revoke**
to invalidate the token on the server.

## Token format

The token is opaque: `bg_cli_<session-uuid>.<32-byte-base64url-secret>`. Only
the HMAC of the secret half is stored on the server. Treat it like a password.

## Endpoint reference

| Method | Path | Auth | Notes |
|---|---|---|---|
| POST | `/oauth/device` | none | Initiate a device authorization. Body: `{"clientLabel":"..."}`. |
| POST | `/oauth/device/poll` | none | Poll. Body: `{"deviceCode":"..."}`. 428 pending, 410 expired, 403 denied, 200 success. |
| GET | `/api/device/lookup?code=XXXX-XXXX` | cookie | SPA-only; shows what's being authorized. |
| POST | `/api/device/approve` | cookie | SPA-only; approves the request. |
| POST | `/api/device/deny` | cookie | SPA-only. |
| GET | `/api/sessions` | cookie or bearer | List your sessions. |
| DELETE | `/api/sessions/{id}` | cookie or bearer | Revoke a session (cannot revoke the calling cookie session). |
