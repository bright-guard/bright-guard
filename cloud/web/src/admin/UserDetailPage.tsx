import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { PlatformUserDetail } from "../api/types";
import { relativeTime } from "../lib/time";
import { describeError } from "./UsersPage";

export default function AdminUserDetailPage() {
  const { id = "" } = useParams();
  const [u, setU] = useState<PlatformUserDetail | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setErr(null);
    try {
      const r = await api<PlatformUserDetail>(`/api/platform/users/${id}`);
      setU(r);
    } catch (e) {
      setErr(describeError(e));
    }
  }, [id]);

  useEffect(() => {
    void load();
  }, [load]);

  const action = async (path: string, init?: RequestInit) => {
    setBusy(true);
    setErr(null);
    try {
      await api<void>(path, init);
      await load();
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  };

  const onSuspendToggle = () => {
    if (!u) return;
    const verb = u.suspendedAt ? "unsuspend" : "suspend";
    void action(`/api/platform/users/${id}/${verb}`, { method: "POST" });
  };

  const onPromoteToggle = () => {
    if (!u) return;
    const verb = u.platformAdmin ? "demote" : "promote";
    void action(`/api/platform/users/${id}/${verb}`, { method: "POST" });
  };

  const onDelete = () => {
    if (!u) return;
    const phrase = `delete user ${u.email}`;
    const got = window.prompt(`Type "${phrase}" to confirm hard-delete:`);
    if (got == null) return;
    void action(`/api/platform/users/${id}`, {
      method: "DELETE",
      body: JSON.stringify({ confirm: got }),
    });
  };

  if (err) return <div className="text-red-400">{err}</div>;
  if (!u) return <div className="text-slate-400">Loading…</div>;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <Link to="/admin/users" className="text-xs text-red-300 hover:underline">
            ← All users
          </Link>
          <h1 className="mt-1 text-2xl font-semibold">{u.email}</h1>
          <div className="text-sm text-slate-400">{u.displayName || "(no name)"}</div>
        </div>
        <div className="flex flex-wrap gap-2">
          <button
            onClick={onSuspendToggle}
            disabled={busy}
            className="rounded-md border border-amber-700 bg-amber-950/40 px-3 py-1.5 text-sm text-amber-200 hover:bg-amber-950"
          >
            {u.suspendedAt ? "Unsuspend" : "Suspend"}
          </button>
          <button
            onClick={onPromoteToggle}
            disabled={busy}
            className="rounded-md border border-red-800 bg-red-950/40 px-3 py-1.5 text-sm text-red-200 hover:bg-red-950"
          >
            {u.platformAdmin ? "Demote admin" : "Promote to admin"}
          </button>
          <button
            onClick={onDelete}
            disabled={busy}
            className="rounded-md border border-red-700 bg-red-900/60 px-3 py-1.5 text-sm text-white hover:bg-red-800"
          >
            Delete…
          </button>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <Stat label="Orgs" value={u.orgCount} />
        <Stat label="Sessions" value={u.sessionCount} />
        <Stat
          label="Last activity"
          value={u.lastActivityAt ? relativeTime(u.lastActivityAt) : "never"}
        />
        <Stat
          label="Status"
          value={
            u.suspendedAt
              ? `suspended ${relativeTime(u.suspendedAt)}`
              : u.platformAdmin
                ? "platform admin"
                : "active"
          }
        />
      </div>

      <div className="rounded-xl border border-slate-800 bg-slate-900/40">
        <div className="border-b border-slate-800 px-4 py-3 text-sm font-semibold uppercase tracking-wide text-slate-400">
          Org memberships
        </div>
        {u.orgs.length === 0 ? (
          <div className="px-4 py-6 text-sm text-slate-500">No orgs.</div>
        ) : (
          <table className="w-full text-sm">
            <thead className="text-xs uppercase tracking-wide text-slate-500">
              <tr>
                <th className="px-4 py-2 text-left">Name</th>
                <th className="px-4 py-2 text-left">Slug</th>
                <th className="px-4 py-2 text-left">Role</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {u.orgs.map((o) => (
                <tr key={o.id}>
                  <td className="px-4 py-2">
                    <Link
                      className="text-red-300 hover:underline"
                      to={`/admin/orgs/${o.id}`}
                    >
                      {o.name}
                    </Link>
                  </td>
                  <td className="px-4 py-2 text-slate-400">{o.slug}</td>
                  <td className="px-4 py-2 text-slate-400">{o.role}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="rounded-xl border border-slate-800 bg-slate-900/40 p-4">
      <div className="text-xs uppercase tracking-wide text-slate-400">{label}</div>
      <div className="mt-1 text-xl font-semibold tabular-nums">{value}</div>
    </div>
  );
}
