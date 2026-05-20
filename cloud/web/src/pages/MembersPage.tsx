import { useCallback, useEffect, useMemo, useState } from "react";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type {
  Invitation,
  Member,
  OrgRole,
} from "../api/types";
import { relativeTime } from "../lib/time";
import PageHelp from "../components/PageHelp";
import NoOrgEmptyState from "../components/NoOrgEmptyState";

type ErrBody = { error?: { code?: string; message?: string } };

function errorMessage(err: unknown): { code?: string; message: string } {
  if (err instanceof ApiError) {
    const b = err.body as ErrBody | string | null;
    if (b && typeof b === "object" && b.error?.message) {
      return { code: b.error.code, message: b.error.message };
    }
    if (typeof b === "string" && b) return { message: b };
    return { message: err.message };
  }
  return { message: err instanceof Error ? err.message : String(err) };
}

export default function MembersPage() {
  const { user, memberships, activeOrgId } = useAuth();
  const active = useMemo(
    () =>
      memberships.find((m) => m.org.id === activeOrgId) ?? memberships[0],
    [memberships, activeOrgId],
  );
  const orgId = active?.org.id;
  const myRole: OrgRole | null = active?.role ?? null;
  const canManage = myRole === "owner" || myRole === "admin";

  const [members, setMembers] = useState<Member[]>([]);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState<OrgRole>("member");
  const [inviting, setInviting] = useState(false);
  const [inviteError, setInviteError] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!orgId) return;
    setLoading(true);
    setError(null);
    try {
      const [mem, inv] = await Promise.all([
        api<Member[]>(`/api/orgs/${orgId}/members`),
        api<Invitation[]>(`/api/orgs/${orgId}/invitations?status=pending`),
      ]);
      setMembers(mem ?? []);
      setInvitations(inv ?? []);
    } catch (err) {
      setError(errorMessage(err).message);
    } finally {
      setLoading(false);
    }
  }, [orgId]);

  useEffect(() => {
    load();
  }, [load]);

  async function onInvite(e: React.FormEvent) {
    e.preventDefault();
    if (!orgId) return;
    setInviteError(null);
    setInviting(true);
    try {
      await api<Invitation>(`/api/orgs/${orgId}/invitations`, {
        method: "POST",
        body: JSON.stringify({ email: inviteEmail.trim(), role: inviteRole }),
      });
      setInviteEmail("");
      setInviteRole("member");
      await load();
    } catch (err) {
      setInviteError(errorMessage(err).message);
    } finally {
      setInviting(false);
    }
  }

  async function revoke(id: string) {
    if (!orgId) return;
    setBusyId(id);
    try {
      await api<void>(`/api/orgs/${orgId}/invitations/${id}`, {
        method: "DELETE",
      });
      await load();
    } catch (err) {
      setError(errorMessage(err).message);
    } finally {
      setBusyId(null);
    }
  }

  if (!orgId) {
    return <NoOrgEmptyState />;
  }

  return (
    <div className="space-y-8">
      <div>
        <div className="flex items-center gap-2">
          <h1 className="text-2xl font-semibold">Members</h1>
          <PageHelp slug="getting-started" />
        </div>
        <p className="mt-1 text-sm text-slate-500">
          Manage who has access to {active?.org.name ?? "this org"}.
        </p>
      </div>

      {error && (
        <div className="rounded-md border border-rose-300 bg-rose-50 px-3 py-2 text-sm text-rose-700">
          {error}
        </div>
      )}

      {canManage && (
        <section className="space-y-3 rounded-xl border border-slate-200 bg-white p-4">
          <h2 className="text-sm font-semibold text-slate-900">Invite teammate</h2>
          <form
            onSubmit={onInvite}
            className="flex flex-col gap-2 sm:flex-row sm:items-end"
          >
            <label className="block flex-1 text-sm">
              <span className="mb-1 block text-slate-500">Email</span>
              <input
                type="email"
                required
                placeholder="teammate@example.com"
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
                className="w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm focus:border-brand-500 focus:outline-none"
              />
            </label>
            <label className="block text-sm">
              <span className="mb-1 block text-slate-500">Role</span>
              <select
                value={inviteRole}
                onChange={(e) => setInviteRole(e.target.value as OrgRole)}
                className="rounded-md border border-slate-300 bg-white px-2 py-2 text-sm focus:border-brand-500 focus:outline-none"
              >
                <option value="member">Member</option>
                <option value="admin">Admin</option>
                <option value="owner">Owner</option>
              </select>
            </label>
            <button
              type="submit"
              disabled={inviting || inviteEmail.trim() === ""}
              className="rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-white hover:bg-brand-400 disabled:opacity-50"
            >
              {inviting ? "Inviting…" : "Invite"}
            </button>
          </form>
          {inviteError && (
            <div className="rounded-md border border-rose-300 bg-rose-50 px-3 py-2 text-sm text-rose-700">
              {inviteError}
            </div>
          )}
        </section>
      )}

      <section className="space-y-3">
        <h2 className="text-sm font-semibold text-slate-900">Members</h2>
        <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
          <table className="min-w-full text-sm">
            <thead className="bg-slate-50 text-left text-xs uppercase tracking-wide text-slate-500">
              <tr>
                <th className="px-4 py-3">Email</th>
                <th className="px-4 py-3">Role</th>
                <th className="px-4 py-3">Joined</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-200">
              {loading && (
                <tr>
                  <td colSpan={3} className="px-4 py-8 text-center text-slate-500">
                    Loading…
                  </td>
                </tr>
              )}
              {!loading && members.length === 0 && (
                <tr>
                  <td colSpan={3} className="px-4 py-8 text-center text-slate-500">
                    No members.
                  </td>
                </tr>
              )}
              {members.map((m) => (
                <tr key={m.userId} className="hover:bg-slate-50">
                  <td className="px-4 py-3 text-slate-900">
                    {m.email}
                    {user && m.userId === user.id && (
                      <span className="ml-2 rounded-md bg-brand-50 px-2 py-0.5 text-xs text-brand-700">
                        you
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-3 text-slate-600">{m.role}</td>
                  <td className="px-4 py-3 text-slate-500">{relativeTime(m.joinedAt)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-semibold text-slate-900">Pending invitations</h2>
        <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
          <table className="min-w-full text-sm">
            <thead className="bg-slate-50 text-left text-xs uppercase tracking-wide text-slate-500">
              <tr>
                <th className="px-4 py-3">Email</th>
                <th className="px-4 py-3">Role</th>
                <th className="px-4 py-3">Invited by</th>
                <th className="px-4 py-3">Expires</th>
                <th className="px-4 py-3"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-200">
              {!loading && invitations.length === 0 && (
                <tr>
                  <td colSpan={5} className="px-4 py-8 text-center text-slate-500">
                    No pending invitations.
                  </td>
                </tr>
              )}
              {invitations.map((inv) => (
                <tr key={inv.id} className="hover:bg-slate-50">
                  <td className="px-4 py-3 text-slate-900">{inv.email}</td>
                  <td className="px-4 py-3 text-slate-600">{inv.role}</td>
                  <td className="px-4 py-3 text-slate-500">{inv.inviterEmail || "—"}</td>
                  <td className="px-4 py-3 text-slate-500">
                    {new Date(inv.expiresAt).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3 text-right">
                    {canManage && (
                      <button
                        onClick={() => revoke(inv.id)}
                        disabled={busyId === inv.id}
                        className="rounded-md border border-slate-300 px-3 py-1 text-xs text-slate-900 hover:bg-slate-100 disabled:opacity-50"
                      >
                        Revoke
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
