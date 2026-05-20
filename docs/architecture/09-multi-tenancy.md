# Multi-Tenancy

One database, one schema, one `orgs` table. Tenant isolation is **enforced at the application layer**, not at the database layer. There is no Postgres row-level-security, no per-tenant schema, no per-tenant connection.

## The model

- A `users` row is global (one identity per Google account).
- An `orgs` row is a tenant.
- An `org_members` row maps user ↔ org with a role (`owner | admin | member`).
- Every tenant-scoped resource has an `org_id NOT NULL REFERENCES orgs(id) ON DELETE CASCADE` (`cloud/api/internal/db/migrations/*.sql` — every table after `00001_init.sql`).

A user can belong to many orgs simultaneously. The "active org" is a per-session field (`sessions.active_org_id`); the SPA shows resources scoped to that one. Switching active org is a single API call (`POST /api/sessions/active-org`).

## How isolation is enforced

Two middlewares stack in series on every tenant route:

1. `auth.RequireUser` — must be signed in.
2. `Server.orgMember` — must be a member of the `orgId` in the URL.

Source: `cloud/api/internal/api/phase2.go:31-56` and `cloud/api/internal/api/server.go:111-156`. Routes are mounted under `/api/orgs/{orgId}/...` so the URL itself carries the tenant. The middleware:

- parses `orgId`,
- runs `Orgs.UserHasMembership(userID, orgID)`,
- on success, stashes the org ID in request context for handlers,
- on failure, returns 403 (or 401 if no user).

Handlers then use `orgFromCtx(r.Context())` and pass it through to the data layer (`cloud/api/internal/store/*.go`). Every store query is parameterized on `org_id`. There is no path-vs-row mismatch — the URL and the query both carry the same tenant.

## RBAC inside an org

`auth.RequireOrgRole` (`cloud/api/internal/auth/role.go:26`) is stacked **on top of** `orgMember` for write operations. Today only invitation create/revoke require `owner | admin` (`cloud/api/internal/api/server.go:151-155`). Everything else — gateways, policies, connections — is open to any member. This is intentional: the operating-plan reserves a "customer-facing roles" workstream for enterprise hardening.

## Cross-tenant routes (none for tenants)

- `/api/me` and `/api/me/invitations` are session-scoped, not org-scoped. They expose only data the calling user already has access to.
- `/api/invitations/{id}/accept` and `/decline` are session-scoped but the invitee may not yet be a member of the inviting org — that's the point. Source: `cloud/api/internal/api/server.go:158-162`.
- `/api/sessions` lists the calling user's own sessions only.

## Platform admin

The one cross-tenant capability is platform admin (`/api/platform/*`). A platform admin can read every org and user, suspend orgs/users, and manage the admin list itself. Every action is recorded in `platform_audit_log` with actor, target, action, and JSON details (`cloud/api/internal/db/migrations/00012_platform_admin.sql:12-22`). See [05-authentication.md § Platform admin](./05-authentication.md#6-platform-admin).

This is the only privileged role that exists. It is bootstrap-seeded from `PLATFORM_ADMIN_SEED_EMAILS`. There is no role beyond it (no support-engineer tier, no read-only auditor).

## Gateway → org mapping

A `gateways` row carries `org_id`. The shim authenticates with a credential that resolves to exactly one gateway row, which carries exactly one `org_id`. The control plane writes any observation under that org. A shim cannot push observations into another org's tables because its credential resolves to a gateway in only one of them (`cloud/api/internal/api/phase2.go:68-86`).

## `mcp_invocations` and `org_callers` are per-org by construction

Both tables have `org_id NOT NULL` and indexes are `(org_id, …)`. There is no cross-org index. There is also no cross-tenant query path in the data-access layer; every store method takes an `orgID` arg.

## Suspension model

A platform admin can suspend an org by writing `orgs.suspended_at` and `_by`. This is a soft state — today **nothing in the API actively checks it** to block reads/writes. Adding a "suspended → 403" gate is a small addition; it lives behind the platform-admin migration (`00012`) but the corresponding enforcement is not yet wired into `orgMember`. Worth knowing if you're evaluating this as an enterprise control.

The same applies to user suspension. **Internal engineers: this is real tech debt; sales should not be claiming "suspension blocks access" until this lands.**

## What this isn't

- **No Postgres-level isolation.** Two orgs share the same connection pool, the same statement cache, the same buffer cache. A SQL injection that bypasses the org-id filter would expose all tenants. The mitigation is that every query uses bind parameters (pgx) and `org_id` is never built from user-supplied text.
- **No per-tenant rate limit, no per-tenant retention.** All tenants share `mcp_invocations`. Partitioning + retention is on the enterprise-hardening roadmap.
- **No data-residency story.** Single-region us-central1.
