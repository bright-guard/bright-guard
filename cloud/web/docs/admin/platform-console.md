# Platform admin console

The `/admin` console is a separate UI for **platform administrators** —
people who run Bright Guard itself, as opposed to tenants of one org. It's
the place to suspend abusive users, delete dead orgs, and read the cross-org
audit log.

## Who's a platform admin?

Platform admins are explicitly granted, not implicit from any tenant role.
The first admin is created out of band (a SQL insert during bootstrap); from
then on, existing platform admins can promote other users from the **Admins**
page.

A user is a platform admin if and only if they have an active row in the
`platform_admins` table. The check (`Platform.IsActiveAdmin`) runs on every
request to a `/api/platform/*` endpoint. Non-admins get a `403 forbidden`.

If you're a platform admin, `/api/me` returns `"platformAdmin": true` and
the SPA's left rail offers an extra **Platform admin** link to `/admin`.
The link is hidden from everyone else.

## Layout

The admin shell uses the same top bar as the tenant SPA but a distinct rail
(`cloud/web/src/admin/AdminShell.tsx`). The rail has four entries:

- **Overview** — high-level counts: total users, total orgs, recent sign-ins.
- **Users** — every user in the system; click into one for a detail page.
- **Orgs** — every org; click into one for a detail page.
- **Admins** — the platform-admins themselves.
- **Audit** — append-only log of every privileged action taken from `/admin`.

## Users page

`/admin/users` lists every account, with email, display name, last sign-in,
and a status badge (active / suspended). From the detail page
(`/admin/users/{id}`) you can:

- **Suspend** — invalidates the user's sessions and prevents new sign-ins.
  The user sees a friendly "your account is suspended" screen on next visit.
- **Unsuspend** — reverses the above.
- **Promote** — adds the user to `platform_admins`.
- **Demote** — removes the user from `platform_admins`.
- **Delete** — soft-deletes the user. They're stripped from every org and
  their sessions are revoked; the row remains for audit-trail integrity.

Every action writes an audit row.

## Orgs page

`/admin/orgs` lists every organization. Click into one for membership, last
activity timestamp, and counts of gateways / MCP servers / policies. The
detail page actions:

- **Suspend** — disables the org. Tenant members see a banner saying the org
  is suspended and can't make API calls scoped to it.
- **Delete** — hard-deletes the org. Cascades to memberships, invitations,
  gateways, connections, policies, and observations.

Deletion is irrecoverable; the audit row is the only artifact left.

## Admins page

`/admin/admins` is the dedicated view of who has platform-admin powers
right now. The actions on a user's row mirror **Promote** / **Demote** from
the user detail page.

A platform admin cannot demote themselves — the API will return `403
forbidden` to prevent locking everyone out. Have a peer demote you.

## Audit log

`/admin/audit` is an append-only feed of every privileged action. Each row
shows:

- **At** — timestamp of the action.
- **Actor** — which admin took the action.
- **Action** — e.g. `user_suspend`, `org_delete`, `admin_promote`.
- **Target** — the user or org affected.
- **Detail** — JSON blob with any extra context the action recorded.

Filters cover actor, target, action type, and time window. There's no
delete or edit on this table — the only way audit rows go away is when the
underlying database is recreated.

## API access

Everything the admin SPA does is also available over the REST API under
`/api/platform/*`. See the [route reference](../reference/routes.md) for the
full list. All endpoints require a session cookie or bearer token belonging
to a platform admin.

## Recovery scenarios

### "I locked myself out as the last admin"

A peer with database access has to re-add you via SQL:

```sql
INSERT INTO platform_admins (user_id, granted_at, granted_by)
VALUES ('<your-user-uuid>', now(), '<peer-user-uuid>');
```

There's no SPA recovery flow for this — the role is meant to be rare.

### "I need to undo a deletion"

Org and user deletions are not reversible from the SPA. If the deletion is
recent, restore from the latest Cloud SQL backup into a scratch instance,
extract the rows you need, and re-insert them via SQL. Treat this as a
break-glass procedure.
