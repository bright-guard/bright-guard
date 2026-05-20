import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { PlatformOrg, PlatformOrgListResp } from "../api/types";
import { relativeTime } from "../lib/time";
import { describeError } from "./UsersPage";

export default function AdminOrgsPage() {
  const [q, setQ] = useState("");
  const [rows, setRows] = useState<PlatformOrg[]>([]);
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
        const r = await api<PlatformOrgListResp>(
          `/api/platform/orgs?${params.toString()}`,
        );
        setRows((prev) => (append ? [...prev, ...(r.items ?? [])] : r.items ?? []));
        setCursor(r.nextCursor);
      } catch (e) {
        setErr(describeError(e));
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
        <h1 className="text-2xl font-semibold">Orgs</h1>
        <input
          className="rounded-md border border-slate-700 bg-slate-900 px-3 py-1.5 text-sm focus:border-red-500 focus:outline-none"
          placeholder="Search name or slug"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") void load(null);
          }}
        />
      </div>

      {err && <div className="text-sm text-red-400">{err}</div>}

      <div className="overflow-x-auto rounded-xl border border-slate-800 bg-slate-900/40">
        <table className="w-full min-w-[1000px] text-sm">
          <thead className="bg-slate-900 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-4 py-2 text-left">Name</th>
              <th className="px-4 py-2 text-left">Slug</th>
              <th className="px-4 py-2 text-right">Members</th>
              <th className="px-4 py-2 text-right">Gateways</th>
              <th className="px-4 py-2 text-right">Servers</th>
              <th className="px-4 py-2 text-right">Connections</th>
              <th className="px-4 py-2 text-left">Created</th>
              <th className="px-4 py-2 text-left">Last activity</th>
              <th className="px-4 py-2 text-left">Flags</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800">
            {rows.map((o) => (
              <tr key={o.id} className="hover:bg-slate-900/50">
                <td className="px-4 py-2">
                  <Link
                    className="text-red-300 hover:underline"
                    to={`/admin/orgs/${o.id}`}
                  >
                    {o.name}
                  </Link>
                </td>
                <td className="px-4 py-2 text-slate-400">{o.slug}</td>
                <td className="px-4 py-2 text-right tabular-nums">{o.memberCount}</td>
                <td className="px-4 py-2 text-right tabular-nums">{o.gatewayCount}</td>
                <td className="px-4 py-2 text-right tabular-nums">{o.mcpServerCount}</td>
                <td className="px-4 py-2 text-right tabular-nums">{o.connectionCount}</td>
                <td className="px-4 py-2 text-slate-400" title={o.createdAt}>
                  {relativeTime(o.createdAt)}
                </td>
                <td className="px-4 py-2 text-slate-400" title={o.lastActivityAt ?? ""}>
                  {o.lastActivityAt ? relativeTime(o.lastActivityAt) : "never"}
                </td>
                <td className="px-4 py-2">
                  {o.suspendedAt && (
                    <span className="rounded bg-amber-950 px-1.5 py-0.5 text-xs text-amber-300 ring-1 ring-amber-800">
                      suspended
                    </span>
                  )}
                </td>
              </tr>
            ))}
            {!loading && rows.length === 0 && (
              <tr>
                <td colSpan={9} className="px-4 py-8 text-center text-slate-500">
                  No orgs match.
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
