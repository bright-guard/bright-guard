# Getting started

This page walks through the first three things every Bright Guard user does:
signing in, creating an organization, and inviting a teammate. The whole flow
takes about five minutes and only needs a browser.

## Sign in

Bright Guard authenticates engineers via Google OAuth. There's no separate
password — your access is tied to the Google account associated with your work
email.

1. Open `https://mcp-governance.infoblox.dev/login`.
2. Click **Continue with Google**.
3. Approve the OAuth consent screen on Google's side. Bright Guard requests
   only the `openid email profile` scopes — no access to your inbox, drive,
   or anything else.

You'll be redirected back to the SPA with a session cookie set. The session
is valid for 30 days unless explicitly revoked from **Settings → Sessions**.

If your deployment doesn't have Google OAuth wired up yet, the **Continue
with Google** button returns a `503 google_oauth_unconfigured`. The platform
admin needs to set `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` on the
service and roll a new revision before sign-in works.

## Create an organization

On first sign-in you'll be sent to `/onboarding` to create your first
organization. An org is the unit of multitenancy: gateways, MCP servers,
policies, and members all live inside one org. A user can belong to several
orgs — pick the active one from the org selector in the top bar.

1. Pick a short name (e.g. your company or BU). You can rename it later from
   the platform-admin console; from the tenant SPA it's currently read-only.
2. Click **Create**. You're now the **owner** of the new org and Bright Guard
   sets it as your active org automatically.

If you were invited to an existing org rather than creating one, you'll see
the invitation banner at the top of the app shell after sign-in. Click
**Review** on the banner to accept or decline.

## Invite a teammate

From the tenant SPA, go to **Members** in the left rail (URL:
`/app/settings/members`). The page lists current members and any pending
invitations.

1. Click **Invite member**.
2. Enter the teammate's email. They don't need a Bright Guard account yet —
   inviting an email that hasn't signed in works too; the invitation will be
   waiting for them after their first Google sign-in.
3. Pick a role: **owner**, **admin**, or **member**.
   - **owner** — everything an admin can do, plus deleting the org.
   - **admin** — invite and remove members, create policies, manage gateways
     and connections.
   - **member** — read-only on configuration, full visibility into activity.
4. Click **Send invite**.

The invitee gets an email with a deep link to the invitation page. If their
email provider isn't reachable from the control plane (which is currently the
case for the demo deployment), the URL is logged to Cloud Logging — copy it
out and share it manually.

## Next steps

- [Install a gateway](gateways/install.md) so Bright Guard starts seeing
  activity from your MCP servers.
- [Add an MCP connection](connections/adding-an-mcp-connection.md) to let the
  control plane talk directly to a remote MCP service.
- [Get a CLI token](cli/device-flow.md) for scripting against the API.
