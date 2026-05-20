import { useCallback, useEffect, useState } from "react";
import { api } from "../api/client";
import type { PlatformAuditEntry, PlatformAuditListResp } from "../api/types";
import { relativeTime } from "../lib/time";
import { describeError } from "./UsersPage";
import PageHelp from "../components/PageHelp";

export default function AdminAuditPage() {
  const [rows, setRows] = useState<PlatformAuditEntry[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);

  const load = useCallback(async (cur: string | null, append = false) => {
    setLoading(true);
    try {
      const params = new URLSearchParams();
      if (cur) params.set("cursor", cur);
      const r = await api<PlatformAuditListResp>(
        `/api/platform/audit?${params.toString()}`,
      );
      setRows((prev) => (append ? [...prev, ...(r.items ?? [])] : r.items ?? []));
      setCursor(r.nextCursor);
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load(null);
  }, [load]);

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <h1 className="text-2xl font-semibold">Audit log</h1>
        <PageHelp slug="admin/platform-console" />
      </div>

      {err && <div className="text-sm text-red-400">{err}</div>}

      <div className="overflow-x-auto rounded-xl border border-slate-800 bg-slate-900/40">
        <table className="w-full min-w-[900px] text-sm">
          <thead className="bg-slate-900 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-4 py-2 text-left">When</th>
              <th className="px-4 py-2 text-left">Actor</th>
              <th className="px-4 py-2 text-left">Action</th>
              <th className="px-4 py-2 text-left">Target</th>
              <th className="px-4 py-2 text-left">Details</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800">
            {rows.map((e) => (
              <tr key={e.id}>
                <td className="px-4 py-2 text-slate-400" title={e.at}>
                  {relativeTime(e.at)}
                </td>
                <td className="px-4 py-2 text-slate-300">{e.actorEmail || "system"}</td>
                <td className="px-4 py-2">
                  <span className="rounded bg-red-950 px-1.5 py-0.5 font-mono text-xs text-red-300">
                    {e.action}
                  </span>
                </td>
                <td className="px-4 py-2 font-mono text-xs text-slate-400">
                  {e.targetKind}/{e.targetId.slice(0, 8)}
                </td>
                <td className="px-4 py-2 font-mono text-xs text-slate-500">
                  {Object.keys(e.details ?? {}).length === 0
                    ? "—"
                    : JSON.stringify(e.details)}
                </td>
              </tr>
            ))}
            {!loading && rows.length === 0 && (
              <tr>
                <td colSpan={5} className="px-4 py-8 text-center text-slate-500">
                  No audit entries yet.
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
