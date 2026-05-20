import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api/client";
import type {
  PlatformUser,
  PlatformUserListResp,
} from "../api/types";
import { relativeTime } from "../lib/time";
import PageHelp from "../components/PageHelp";

export default function AdminUsersPage() {
  const [q, setQ] = useState("");
  const [rows, setRows] = useState<PlatformUser[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);

  const load = useCallback(
    async (cur: string | null, append = false) => {
      setLoading(true);
      try {
        const params = new URLSearchParams();
        if (q) params.set("q", q);
        if (cur) params.set("cursor", cur);
        const r = await api<PlatformUserListResp>(
          `/api/platform/users?${params.toString()}`,
        );
        setRows((prev) => (append ? [...prev, ...(r.items ?? [])] : r.items ?? []));
        setCursor(r.nextCursor);
      } catch (e) {
        setErr(e instanceof Error ? e.message : String(e));
      } finally {
        setLoading(false);
      }
    },
    [q],
  );

  useEffect(() => {
    void load(null);
  }, [load]);

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h1 className="text-2xl font-semibold">Users</h1>
          <PageHelp slug="admin/platform-console" />
        </div>
        <input
          className="rounded-md border border-slate-700 bg-slate-900 px-3 py-1.5 text-sm focus:border-red-500 focus:outline-none"
          placeholder="Search email or name"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") void load(null);
          }}
        />
      </div>

      {err && <div className="text-sm text-red-400">{err}</div>}

      <div className="overflow-x-auto rounded-xl border border-slate-800 bg-slate-900/40">
        <table className="w-full min-w-[900px] text-sm">
          <thead className="bg-slate-900 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-4 py-2 text-left">Email</th>
              <th className="px-4 py-2 text-left">Name</th>
              <th className="px-4 py-2 text-right">Orgs</th>
              <th className="px-4 py-2 text-left">Created</th>
              <th className="px-4 py-2 text-left">Last seen</th>
              <th className="px-4 py-2 text-left">Flags</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800">
            {rows.map((u) => (
              <tr key={u.id} className="hover:bg-slate-900/50">
                <td className="px-4 py-2">
                  <Link
                    className="text-red-300 hover:underline"
                    to={`/admin/users/${u.id}`}
                  >
                    {u.email}
                  </Link>
                </td>
                <td className="px-4 py-2 text-slate-300">{u.displayName || "—"}</td>
                <td className="px-4 py-2 text-right tabular-nums">{u.orgCount}</td>
                <td className="px-4 py-2 text-slate-400" title={u.createdAt}>
                  {relativeTime(u.createdAt)}
                </td>
                <td className="px-4 py-2 text-slate-400" title={u.lastSeenAt ?? ""}>
                  {u.lastSeenAt ? relativeTime(u.lastSeenAt) : "never"}
                </td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    {u.platformAdmin && (
                      <span className="rounded bg-red-950 px-1.5 py-0.5 text-xs text-red-300 ring-1 ring-red-800">
                        admin
                      </span>
                    )}
                    {u.suspendedAt && (
                      <span className="rounded bg-amber-950 px-1.5 py-0.5 text-xs text-amber-300 ring-1 ring-amber-800">
                        suspended
                      </span>
                    )}
                  </div>
                </td>
              </tr>
            ))}
            {!loading && rows.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-slate-500">
                  No users match.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      <div className="flex items-center justify-between text-xs text-slate-500">
        <div>{rows.length} loaded</div>
        {cursor && (
          <button
            className="rounded-md border border-slate-700 px-3 py-1 hover:border-slate-500"
            onClick={() => void load(cursor, true)}
            disabled={loading}
          >
            Load more
          </button>
        )}
      </div>
    </div>
  );
}

// Helper exported for the user-detail page so it can also surface API errors.
export function describeError(e: unknown): string {
  if (e instanceof ApiError) return `${e.status}: ${typeof e.body === "string" ? e.body : (e.body as any)?.error ?? e.message}`;
  if (e instanceof Error) return e.message;
  return String(e);
}
