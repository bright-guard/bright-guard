import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type {
  PlatformAdmin,
  PlatformAdminListResp,
  PlatformUser,
  PlatformUserListResp,
} from "../api/types";
import { relativeTime } from "../lib/time";
import { describeError } from "./UsersPage";
import PageHelp from "../components/PageHelp";

export default function AdminAdminsPage() {
  const { user } = useAuth();
  const [admins, setAdmins] = useState<PlatformAdmin[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [query, setQuery] = useState("");
  const [matches, setMatches] = useState<PlatformUser[]>([]);

  const load = useCallback(async () => {
    setErr(null);
    try {
      const r = await api<PlatformAdminListResp>("/api/platform/admins");
      setAdmins(r.items ?? []);
    } catch (e) {
      setErr(describeError(e));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!query) {
      setMatches([]);
      return;
    }
    let cancelled = false;
    const t = setTimeout(async () => {
      try {
        const r = await api<PlatformUserListResp>(
          `/api/platform/users?q=${encodeURIComponent(query)}&limit=20`,
        );
        if (!cancelled) setMatches(r.items ?? []);
      } catch {
        if (!cancelled) setMatches([]);
      }
    }, 200);
    return () => {
      cancelled = true;
      clearTimeout(t);
    };
  }, [query]);

  const onPromote = async (u: PlatformUser) => {
    setBusy(true);
    setErr(null);
    try {
      await api<void>(`/api/platform/users/${u.id}/promote`, { method: "POST" });
      setQuery("");
      setMatches([]);
      await load();
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  };

  const onDemote = async (a: PlatformAdmin) => {
    if (!window.confirm(`Demote ${a.email}?`)) return;
    setBusy(true);
    setErr(null);
    try {
      await api<void>(`/api/platform/users/${a.userId}/demote`, { method: "POST" });
      await load();
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  };

  const activeCount = admins.length;

  return (
    <div className="space-y-6">
      <div>
        <div className="flex items-center gap-2">
          <h1 className="text-2xl font-semibold">Platform admins</h1>
          <PageHelp slug="admin/platform-console" />
        </div>
        <p className="mt-1 text-sm text-slate-400">
          Active administrators. The last active admin cannot demote themselves.
        </p>
      </div>

      {err && <div className="text-sm text-red-400">{err}</div>}

      <div className="overflow-x-auto rounded-xl border border-slate-800 bg-slate-900/40">
        <table className="w-full text-sm">
          <thead className="bg-slate-900 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-4 py-2 text-left">Email</th>
              <th className="px-4 py-2 text-left">Added by</th>
              <th className="px-4 py-2 text-left">Added</th>
              <th className="px-4 py-2 text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800">
            {admins.map((a) => {
              const isSelf = user?.id === a.userId;
              const isLast = activeCount <= 1;
              const blockSelfLast = isSelf && isLast;
              return (
                <tr key={a.userId}>
                  <td className="px-4 py-2">
                    <Link
                      className="text-red-300 hover:underline"
                      to={`/admin/users/${a.userId}`}
                    >
                      {a.email}
                    </Link>
                    {isSelf && (
                      <span className="ml-2 text-xs text-slate-500">(you)</span>
                    )}
                  </td>
                  <td className="px-4 py-2 text-slate-400">
                    {a.addedBy ? a.addedByEmail : "system"}
                  </td>
                  <td className="px-4 py-2 text-slate-400" title={a.addedAt}>
                    {relativeTime(a.addedAt)}
                  </td>
                  <td className="px-4 py-2 text-right">
                    <button
                      onClick={() => onDemote(a)}
                      disabled={busy || blockSelfLast}
                      title={
                        blockSelfLast
                          ? "Cannot demote the last active platform admin"
                          : ""
                      }
                      className="rounded-md border border-red-800 bg-red-950/40 px-3 py-1 text-xs text-red-200 hover:bg-red-950 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      Demote
                    </button>
                  </td>
                </tr>
              );
            })}
            {admins.length === 0 && (
              <tr>
                <td colSpan={4} className="px-4 py-8 text-center text-slate-500">
                  No active admins.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      <div className="rounded-xl border border-slate-800 bg-slate-900/40 p-4">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-slate-400">
          Promote an existing user
        </h2>
        <p className="mt-1 text-xs text-slate-500">
          Search by email. Only registered users can be promoted.
        </p>
        <input
          className="mt-3 w-full rounded-md border border-slate-700 bg-slate-900 px-3 py-1.5 text-sm focus:border-red-500 focus:outline-none"
          placeholder="Search user email…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        {matches.length > 0 && (
          <ul className="mt-2 divide-y divide-slate-800 rounded-md border border-slate-800">
            {matches.map((u) => (
              <li
                key={u.id}
                className="flex items-center justify-between px-3 py-2 text-sm"
              >
                <div>
                  <div className="text-slate-200">{u.email}</div>
                  <div className="text-xs text-slate-500">{u.displayName}</div>
                </div>
                <button
                  onClick={() => onPromote(u)}
                  disabled={busy || u.platformAdmin}
                  className="rounded-md border border-red-700 bg-red-900/60 px-3 py-1 text-xs text-white hover:bg-red-800 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {u.platformAdmin ? "Already admin" : "Promote"}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
