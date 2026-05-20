# Authentication

Five auth surfaces, sharing one `sessions` table where possible.

## 1. Google OIDC (browser sign-in)

The only end-user sign-in path in prod. `auth.NewGoogle` discovers `https://accounts.google.com` via go-oidc and verifies ID tokens against Google's JWKS (`cloud/api/internal/auth/google.go:40-54`).

Sign-in flow:

1. SPA navigates to `/auth/google/start`. Handler picks a redirect URI scoped to the current host (rejecting hosts not in `ALLOWED_HOSTS`) and sets a one-time `bg_oauth_state` cookie (`cloud/api/internal/auth/google.go:59-65`).
2. User completes consent at Google; Google redirects to `/auth/google/callback?state=...&code=...`.
3. Handler verifies state, exchanges code for tokens, verifies the ID token, upserts the user row by `google_subject`, mints a session row, sets `bg_session` cookie.

Sessions live 30 days (`cloud/api/cmd/api/main.go:26`). Cookie attributes: `HttpOnly`, `Secure` (prod), `SameSite=Lax` (`cloud/api/cmd/api/main.go:122-125`).

**Multi-host gotcha.** `redirectURIFor` reads `r.Host`; the host must be on `ALLOWED_HOSTS` (`cloud/api/internal/config/config.go:128-138`). That's how the service simultaneously serves `mcp-governance.infoblox.dev` and the raw `*.run.app` URL.

## 2. Cookie session (browser)

After Google sign-in, every subsequent request is authenticated by the `bg_session` cookie. `auth.Middleware` (`cloud/api/internal/auth/session.go:82`) attempts cookie auth first, then bearer; either path sets `user` and `session` in request context and touches the session row.

Cookie sessions have `kind = 'cookie'` (the default). The middleware refuses to accept a cookie if the row's `kind` is anything else (`cloud/api/internal/auth/session.go:117-120`), so a stolen CLI bearer cannot be replayed as a cookie.

## 3. Device-code flow (CLI / terminal)

Spec'd in `cloud/api/internal/api/device.go`. Public endpoints:

- `POST /oauth/device` — issues a `deviceCode` (server-only) and a human-readable `userCode` like `ABCD-WXYZ` from an unambiguous alphabet (`cloud/api/internal/api/device.go:24-26`). TTL 10 minutes, poll interval 5s.
- `POST /oauth/device/poll` — returns `428 authorization_pending` until the human approves; then `200 {accessToken: bg_cli_<uuid>.<secret>}`. Single-use — the row is consumed on first successful poll.

Approval requires a signed-in browser. The SPA's `/device?code=...` route hits `POST /api/device/approve` (session-protected) which materializes a CLI session row (`kind='cli'`, `token_hash=H(secret)`, label from initiate request) and stashes the bearer in `device_authorizations.bearer_secret` for the CLI to pick up exactly once.

See [04-request-flows.md § device-code](./04-request-flows.md#4-oauth2-device-code-cli-sign-in).

## 4. Bearer auth (`bg_cli_<uuid>.<secret>`)

`auth.tryBearer` (`cloud/api/internal/auth/session.go:128-147`) parses the bearer, splits at the first `.`, and looks up the session by UUID + HMAC of the secret. Bearer auth is honored on every `/api/*` route that the cookie is. A revoked or expired session row makes both paths fail.

The bearer format is `bg_cli_` prefix + UUID + `.` + 32-byte base64url secret (`cloud/api/internal/auth/session.go:19`). The prefix exists so accidental log exposure is visually obvious — a secret-scanning rule can be written against `bg_cli_`.

## 5. Gateway bearer (shim → control plane)

The shim's credential is *not* a CLI bearer. It is a separate token shape returned by `/v1/gateway/register`. Validation:

- `gatewayBearer` middleware (`cloud/api/internal/api/phase2.go:68-86`) requires `Authorization: Bearer <credential>`.
- `Gateways.AuthenticateCredential` looks the credential up against `gateway_credentials.secret_hash` (no plaintext stored).
- On success: `TouchSeen` advances `gateways.last_seen_at`; `CommitEnrollmentOnHeartbeat` permanently consumes the original enrollment token (idempotent; covers shim-crash-between-claim-and-persist).

`/v1/gateway/register` is intentionally bearer-less — it accepts the enrollment token in the JSON body (`cloud/api/internal/api/server.go:188`). An invalid Authorization header on `register` is silently ignored.

## 6. Platform admin

Backoffice routes under `/api/platform/*` are gated by `auth.RequirePlatformAdmin` stacked on `auth.RequireUser` (`cloud/api/internal/api/server.go:167-184`). A user is a platform admin if:

- their row is in `platform_admins` with `revoked_at IS NULL`, OR
- their email matches `PLATFORM_ADMIN_SEED_EMAILS` (set via env; default seed list at `cloud/api/internal/config/config.go:33-36`).

Seed-list members are auto-promoted on sign-in. Promotion / demotion is itself a platform-admin operation and writes to `platform_audit_log`.

## Org-level RBAC

`auth.RequireOrgRole` (`cloud/api/internal/auth/role.go:26`) is layered on top of `orgMember`. The role set is `owner | admin | member` (`cloud/api/internal/db/migrations/00001_init.sql:25`). Write operations on invitations require `owner | admin`; everything else is open to any member. There is no per-resource ACL — membership of an org grants read on every resource scoped to it.

## Session management UI

Authenticated users can list every session they hold (cookie + CLI) at `/app/settings/sessions` and revoke individual rows. Current session cannot revoke itself (`cloud/api/internal/api/device.go:330-337`). This is the only knob in the product for "log me out everywhere."

## What is not yet here

- No SAML / OIDC SSO beyond Google.
- No IdP-driven group sync; `org_role` is application-managed.
- No per-resource ACLs.
- No MFA enforcement step (Google's MFA suffices upstream; the control plane has no second factor of its own).
