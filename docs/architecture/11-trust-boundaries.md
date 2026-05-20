# Trust Boundaries

A walk through the surfaces that matter for a security review.

## TLS termination

- **All public traffic terminates at the Google Frontend (GFE).** Cloud Run accepts only HTTPS at the load-balancer edge; the container behind it speaks plain HTTP on `:8080`. The `Strict-Transport-Security` header (`cloud/api/internal/api/server.go:369`) tells browsers to refuse non-HTTPS for 2 years.
- **Custom domain `mcp-governance.infoblox.dev`** maps to `ghs.googlehosted.com` via Cloud Run domain mappings; GFE handles the cert.
- **No mTLS** today. The gateway shim authenticates by Bearer token, not certificate.
- **Cloud SQL** is reached over the unix socket provisioned by the Cloud SQL connector, not over the network — there is no DB hostname or port exposed.

## What the customer sees

- The SPA bundle (HTML, JS, CSS) — public, embedded in the Go binary.
- `/api/*` responses for orgs they belong to, scoped by `org_id`.
- Their gateway's enrollment token and credential, once, at install time.
- Their own policy expressions and the activity rows for their org.

The customer does **not** see:
- Other tenants' data (enforced application-side; see [09-multi-tenancy.md](./09-multi-tenancy.md)).
- The `platform_audit_log`.
- Other customers' shim credentials, gateway IDs, or invocation rows.
- Secret Manager contents.

## What the platform operator sees

- All orgs and users, via `/api/platform/*` (platform admin role required, see [05-authentication.md § Platform admin](./05-authentication.md#6-platform-admin)).
- Cloud Logging across all tenants.
- The full database via `gcloud sql connect`.
- Secret Manager contents (with project IAM).

There is no separation between "operator" and "customer support" today. A platform admin is fully privileged.

## Secrets handling

| Secret | At rest | In transit | Notes |
|---|---|---|---|
| `SESSION_SECRET` | Secret Manager `session-secret` | Injected as env var at container start | Used as HMAC key for AEAD encryption of `mcp_connections.auth_state`, for session ID HMAC, and for the gateway credential HMAC. ≥32 chars enforced. |
| `db-password` | Secret Manager | Substituted into `DATABASE_URL` by `deploy.sh` (`cloud/deploy/deploy.sh:40-41`) | The script reads the password into a shell variable at deploy time; the running container only sees the assembled DSN. |
| `google-client-secret` | Secret Manager | Env var via `--update-secrets` | Used in OAuth code exchange only. |
| `bg-shim-bg-credential` | Secret Manager | Env var on `bright-guard-shim-demo` | The demo shim's bearer. Real customer shims persist their credential to `/data/credential` (0600). |
| `mcp_connections.auth_state` | AEAD-encrypted, stored as `bytea` in Postgres | Decrypted in-process when discovery runs | AEAD key is derived from `SESSION_SECRET` (`cloud/api/cmd/api/main.go:66-69`). Plaintext is never on disk or in logs. |
| `gateway_credentials.secret_hash` | Hashed `bytea` in Postgres | Plaintext returned once at register, never stored | A leaked DB does not leak gateway credentials. |
| OAuth tokens for connected MCP servers | Inside the AEAD-encrypted `auth_state` blob | Decrypted in-process to round-trip into the third-party server | Refresh handled by `OAuth2RoundTripper` (`cloud/api/internal/mcp/`). |

The shim credential and the CLI bearer are the only long-lived bearer tokens. Both are session-row-backed; revoking the session row revokes the bearer.

## Threat surfaces

### 1. Gateway register endpoint

`POST /v1/gateway/register` is the only **unauthenticated** write endpoint that touches the data plane. It accepts an enrollment token in the body and exchanges it for a credential. Risks:

- **Token guessing.** Enrollment tokens are 32-byte random secrets, base64-encoded; the column stores `token_hash bytea`. Guessing is infeasible.
- **Token replay.** Single-use; `commit_pending` becomes `false` on first heartbeat. Replays return `409 conflict`.
- **Phishing.** A customer convinced to paste their token into an attacker's environment lets the attacker register and emit observations into the org. Mitigation today: token TTL is 24h (`cloud/api/internal/api/phase2.go:28`); install command shown in the UI is the canonical form.

### 2. CSP and the SPA

The control plane is its own SPA host. Strict CSP (`cloud/api/internal/api/server.go:372-380`):
- `default-src 'self'` — no third-party JS, CSS, or fonts except as listed.
- `frame-ancestors 'none'` — clickjacking-resistant.
- `connect-src 'self'` — no fetch off-origin from the SPA.
- `style-src 'self' 'unsafe-inline' https://fonts.googleapis.com` — `unsafe-inline` is the one weakness, needed for current styling. A nonce-based migration is a follow-up.
- `form-action 'self'` — limits where forms can submit to.

`X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`.

### 3. CORS

`corsMiddleware` allows the origin only if it matches `WebBaseURL` exactly (`cloud/api/internal/api/server.go:204-223`). Credentials are allowed. No wildcard. There is no per-tenant CORS — the assumption is single-host for the API.

### 4. OAuth2 callback

`/oauth/connect/callback` is intentionally cookie-less. Trust is in the `state` parameter, which resolves to an `oauth_authcode_states` row whose `code_verifier` (PKCE) is required to complete the token exchange. State rows expire after a few minutes.

### 5. CEL injection / DoS

`policies.expression` is tenant-supplied CEL. The cost limit (`evalCostLimit = 50_000`, `cloud/api/internal/policy/policy.go:26`) bounds CPU per eval. `cel.ext.Strings()` is **not** enabled because its regex helpers can be DoS'd via backtracking (`cloud/api/internal/policy/policy.go:5-8`). Compilation runs at policy create/update; runtime errors are swallowed as non-matches.

### 6. Caller JSON

`mcp_invocations.caller` is shim-supplied `jsonb`. CEL dot-indexing tolerates missing keys (returns no match). There is no size limit on the body of `/v1/gateway/observations` other than chi's defaults. A misbehaving shim can fill the table; today there is no per-org rate limit on this endpoint.

## Threat surfaces not yet addressed

- **Per-org rate limiting.** No throttle on observations ingest. A noisy or malicious gateway could fill `mcp_invocations` for its own org. (Cross-org isolation still holds.)
- **Bundle signing.** The policy bundle is delivered over TLS but unsigned. If an operator's Cloud Run service were compromised, attacker-authored bundles would be honored by customer shims. Bundle signing is on the roadmap (epic #13).
- **Suspension not enforced.** `orgs.suspended_at` is set by platform admins but no middleware blocks reads/writes on suspended orgs today. See [09-multi-tenancy.md § Suspension model](./09-multi-tenancy.md#suspension-model).
- **No SBOM, no SLSA provenance.** Distroless runtime is a good baseline; supply-chain attestation is not yet wired.
- **No SOC 2.** Type II prep is on the operating-plan; not in audit today.

## Defense-in-depth wins that are easy to miss

- Sessions table tracks `kind`; a cookie cannot impersonate a CLI session (`cloud/api/internal/auth/session.go:117-120`).
- Bearer prefix `bg_cli_` enables secret-scanning rules.
- Distroless final image (no shell, no busybox).
- All-pgx; bind parameters everywhere; no string-built SQL paths in the data layer.
- `securityHeadersMiddleware` runs first so headers are applied to error responses too (`cloud/api/internal/api/server.go:51-52`).
