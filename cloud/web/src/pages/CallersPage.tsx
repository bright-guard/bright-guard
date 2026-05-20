import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { OrgCaller, OrgCallerListResp } from "../api/types";
import { relativeTime } from "../lib/time";

type FilterMode = "all" | "new";

function buildQuery(orgId: string, params: Record<string, string>) {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v) qs.set(k, v);
  }
  const s = qs.toString();
  const base = `/api/orgs/${orgId}/callers`;
  return s ? `${base}?${s}` : base;
}

export default function CallersPage() {
  const { activeOrgId } = useAuth();
  const [mode, setMode] = useState<FilterMode>("all");
  const [search, setSearch] = useState("");
  const [debounced, setDebounced] = useState("");

  const [rows, setRows] = useState<OrgCaller[]>([]);
  const [totals, setTotals] = useState<{ total: number; flaggedNew: number }>({
    total: 0,
    flaggedNew: 0,
  });
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(search), 300);
    return () => window.clearTimeout(t);
  }, [search]);

  const reqSeq = useRef(0);
  const params = useMemo(
    () => ({
      flagged: mode === "new" ? "true" : "",
      q: debounced.trim(),
    }),
    [mode, debounced],
  );

  useEffect(() => {
    if (!activeOrgId) return;
    const mySeq = ++reqSeq.current;
    setLoading(true);
    setError(null);
    api<OrgCallerListResp>(buildQuery(activeOrgId, params))
      .then((res) => {
        if (mySeq !== reqSeq.current) return;
        setRows(res.items ?? []);
        setNextCursor(res.nextCursor ?? null);
        setTotals(res.totals);
      })
      .catch((err) => {
        if (mySeq !== reqSeq.current) return;
        setError(String(err));
      })
      .finally(() => {
        if (mySeq === reqSeq.current) setLoading(false);
      });
  }, [activeOrgId, params]);

  async function loadMore() {
    if (!activeOrgId || !nextCursor) return;
    setLoadingMore(true);
    try {
      const res = await api<OrgCallerListResp>(
        buildQuery(activeOrgId, { ...params, cursor: nextCursor }),
      );
      setRows((prev) => [...prev, ...(res.items ?? [])]);
      setNextCursor(res.nextCursor ?? null);
    } catch (err) {
      setError(String(err));
    } finally {
      setLoadingMore(false);
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Callers</h1>
        <p className="mt-1 text-sm text-slate-400">
          Distinct AI agents and identities observed invoking MCP servers in this org.
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <div className="flex gap-1 rounded-md border border-slate-800 bg-slate-900/40 p-1">
          <button
            onClick={() => setMode("all")}
            className={`rounded px-3 py-1 text-xs ${
              mode === "all" ? "bg-slate-800 text-white" : "text-slate-400 hover:text-slate-200"
            }`}
          >
            All
          </button>
          <button
            onClick={() => setMode("new")}
            className={`rounded px-3 py-1 text-xs ${
              mode === "new" ? "bg-slate-800 text-white" : "text-slate-400 hover:text-slate-200"
            }`}
          >
            New (last 7d)
          </button>
        </div>

        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search caller label…"
          className="ml-auto w-64 rounded-md border border-slate-700 bg-slate-900 px-3 py-1.5 text-sm focus:border-brand-500 focus:outline-none"
        />
      </div>

      <div className="flex flex-wrap items-center gap-x-6 gap-y-1 rounded-xl border border-slate-800 bg-slate-900/40 px-4 py-3 text-sm">
        <span className="text-slate-400">
          <span className="font-semibold text-slate-200">{totals.total}</span> total
        </span>
        <span className="text-amber-300">{totals.flaggedNew} new</span>
      </div>

      {error && (
        <div className="rounded-md border border-rose-900/60 bg-rose-950/40 px-4 py-3 text-sm text-rose-300">
          {error}
        </div>
      )}

      <div className="overflow-hidden rounded-xl border border-slate-800 bg-slate-900/40">
        <table className="min-w-full text-sm">
          <thead className="bg-slate-900/60 text-left text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-4 py-3">Caller</th>
              <th className="px-4 py-3">First seen</th>
              <th className="px-4 py-3">Last seen</th>
              <th className="px-4 py-3 text-right">Invocations</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800">
            {loading && rows.length === 0 && (
              <tr>
                <td colSpan={4} className="px-4 py-8 text-center text-slate-500">
                  Loading…
                </td>
              </tr>
            )}
            {!loading && rows.length === 0 && (
              <tr>
                <td colSpan={4} className="px-4 py-10 text-center text-slate-500">
                  No callers tracked yet — invocations populate this list within 5
                  minutes of being recorded.
                </td>
              </tr>
            )}
            {rows.map((r) => (
              <tr key={r.id} className="hover:bg-slate-900/40">
                <td className="px-4 py-3">
                  <Link
                    to={`/app/callers/${r.id}`}
                    className="flex items-center gap-2 text-brand-300 hover:underline"
                  >
                    <span>{r.label || "(anonymous)"}</span>
                    {r.flaggedNew && (
                      <span className="rounded-full bg-amber-900/50 px-2 py-0.5 text-[10px] uppercase tracking-wide text-amber-200">
                        NEW
                      </span>
                    )}
                  </Link>
                  <div
                    className="mt-0.5 font-mono text-[11px] text-slate-500"
                    title={r.signature}
                  >
                    {r.signature.slice(0, 12)}…
                  </div>
                </td>
                <td className="px-4 py-3 text-slate-400" title={r.firstSeenAt}>
                  {relativeTime(r.firstSeenAt)}
                </td>
                <td className="px-4 py-3 text-slate-400" title={r.lastSeenAt}>
                  {relativeTime(r.lastSeenAt)}
                </td>
                <td className="px-4 py-3 text-right text-slate-200">
                  {r.invocationCount.toLocaleString()}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {nextCursor && (
        <div className="flex justify-center">
          <button
            onClick={loadMore}
            disabled={loadingMore}
            className="rounded-md border border-slate-700 bg-slate-900 px-4 py-1.5 text-sm text-slate-200 hover:border-slate-600 disabled:opacity-50"
          >
            {loadingMore ? "Loading…" : "Load more"}
          </button>
        </div>
      )}

      <p className="text-xs text-slate-500">
        Callers flagged "new" are identities first observed in the last 7 days.
        Acknowledge a caller from its detail page to clear the badge.
      </p>
    </div>
  );
}
