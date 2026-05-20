import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type {
  ActivityListResp,
  ActivityRow,
  ActivitySummary,
} from "../api/types";
import { relativeTime } from "../lib/time";

type Window = "1h" | "24h" | "7d";
const WINDOWS: { id: Window; label: string; ms: number }[] = [
  { id: "1h", label: "Last hour", ms: 60 * 60 * 1000 },
  { id: "24h", label: "Last 24h", ms: 24 * 60 * 60 * 1000 },
  { id: "7d", label: "Last 7d", ms: 7 * 24 * 60 * 60 * 1000 },
];
const KINDS = ["tool", "resource", "prompt"] as const;
const STATUSES = ["ok", "error", "denied"] as const;

type Kind = (typeof KINDS)[number];
type Status = (typeof STATUSES)[number];

function statusClasses(status: string): string {
  switch (status) {
    case "ok":
      return "bg-emerald-900/50 text-emerald-300";
    case "error":
      return "bg-rose-900/50 text-rose-300";
    case "denied":
      return "bg-rose-900/60 text-rose-200 uppercase tracking-wide";
    default:
      return "bg-slate-800 text-slate-300";
  }
}

function statusLabel(status: string): string {
  if (status === "denied") return "DENIED";
  return status;
}

function statusTitle(status: string): string | undefined {
  if (status === "denied") return "denied by policy";
  return undefined;
}

function buildQuery(orgId: string, params: Record<string, string | string[]>) {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (Array.isArray(v)) {
      for (const item of v) if (item) qs.append(k, item);
    } else if (v) {
      qs.set(k, v);
    }
  }
  const s = qs.toString();
  const base = `/api/orgs/${orgId}/activity`;
  return s ? `${base}?${s}` : base;
}

function truncate(s: string, n = 80): string {
  if (s.length <= n) return s;
  return s.slice(0, n - 1) + "…";
}

export default function ActivityPage() {
  const { activeOrgId } = useAuth();
  const [windowId, setWindowId] = useState<Window>("24h");
  const [kinds, setKinds] = useState<Set<Kind>>(new Set());
  const [statuses, setStatuses] = useState<Set<Status>>(new Set());
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");

  const [rows, setRows] = useState<ActivityRow[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [summary, setSummary] = useState<ActivitySummary | null>(null);

  // 300ms debounce on the search input
  useEffect(() => {
    const t = window.setTimeout(() => setDebouncedSearch(search), 300);
    return () => window.clearTimeout(t);
  }, [search]);

  const windowMs = useMemo(
    () => WINDOWS.find((w) => w.id === windowId)!.ms,
    [windowId],
  );

  // Anchor the from/to to the most recent change in filters to keep paging stable.
  const range = useMemo(() => {
    const now = new Date();
    return {
      to: now.toISOString(),
      from: new Date(now.getTime() - windowMs).toISOString(),
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [windowMs, kinds, statuses, debouncedSearch, activeOrgId]);

  const reqSeq = useRef(0);

  useEffect(() => {
    if (!activeOrgId) return;
    const mySeq = ++reqSeq.current;
    setLoading(true);
    setError(null);

    const params: Record<string, string | string[]> = {
      from: range.from,
      to: range.to,
      capabilityKind: Array.from(kinds),
      status: Array.from(statuses),
    };
    if (debouncedSearch.trim()) params.q = debouncedSearch.trim();

    const listUrl = buildQuery(activeOrgId, params);
    const sumQs = new URLSearchParams({ from: range.from, to: range.to });
    const sumUrl = `/api/orgs/${activeOrgId}/activity/summary?${sumQs.toString()}`;

    Promise.all([
      api<ActivityListResp>(listUrl),
      api<ActivitySummary>(sumUrl),
    ])
      .then(([list, sum]) => {
        if (mySeq !== reqSeq.current) return;
        setRows(list.items ?? []);
        setNextCursor(list.nextCursor ?? null);
        setSummary(sum);
      })
      .catch((err) => {
        if (mySeq !== reqSeq.current) return;
        setError(String(err));
      })
      .finally(() => {
        if (mySeq === reqSeq.current) setLoading(false);
      });
  }, [activeOrgId, range, kinds, statuses, debouncedSearch]);

  function toggle<T>(set: Set<T>, value: T): Set<T> {
    const next = new Set(set);
    if (next.has(value)) next.delete(value);
    else next.add(value);
    return next;
  }

  async function loadMore() {
    if (!activeOrgId || !nextCursor) return;
    setLoadingMore(true);
    try {
      const params: Record<string, string | string[]> = {
        from: range.from,
        to: range.to,
        capabilityKind: Array.from(kinds),
        status: Array.from(statuses),
        cursor: nextCursor,
      };
      if (debouncedSearch.trim()) params.q = debouncedSearch.trim();
      const list = await api<ActivityListResp>(buildQuery(activeOrgId, params));
      setRows((prev) => [...prev, ...(list.items ?? [])]);
      setNextCursor(list.nextCursor ?? null);
    } catch (err) {
      setError(String(err));
    } finally {
      setLoadingMore(false);
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Activity</h1>
        <p className="mt-1 text-sm text-slate-400">
          Org-wide timeline of MCP invocations.
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <div className="flex gap-1 rounded-md border border-slate-800 bg-slate-900/40 p-1">
          {WINDOWS.map((w) => (
            <button
              key={w.id}
              onClick={() => setWindowId(w.id)}
              className={`rounded px-3 py-1 text-xs ${
                windowId === w.id
                  ? "bg-slate-800 text-white"
                  : "text-slate-400 hover:text-slate-200"
              }`}
            >
              {w.label}
            </button>
          ))}
        </div>

        <div className="flex flex-wrap gap-1">
          {KINDS.map((k) => (
            <Chip
              key={k}
              active={kinds.has(k)}
              onClick={() => setKinds((s) => toggle(s, k))}
            >
              {k}
            </Chip>
          ))}
        </div>

        <div className="flex flex-wrap gap-1">
          {STATUSES.map((s) => (
            <Chip
              key={s}
              active={statuses.has(s)}
              onClick={() => setStatuses((cur) => toggle(cur, s))}
              tone={s}
            >
              {s}
            </Chip>
          ))}
        </div>

        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search capability or server…"
          className="ml-auto w-64 rounded-md border border-slate-700 bg-slate-900 px-3 py-1.5 text-sm focus:border-brand-500 focus:outline-none"
        />
      </div>

      <SummaryCard summary={summary} />

      {error && (
        <div className="rounded-md border border-rose-900/60 bg-rose-950/40 px-4 py-3 text-sm text-rose-300">
          {error}
        </div>
      )}

      <div className="overflow-hidden rounded-xl border border-slate-800 bg-slate-900/40">
        <table className="min-w-full text-sm">
          <thead className="bg-slate-900/60 text-left text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-4 py-3">When</th>
              <th className="px-4 py-3">MCP server</th>
              <th className="px-4 py-3">Capability</th>
              <th className="px-4 py-3">Status</th>
              <th className="px-4 py-3">Latency</th>
              <th className="px-4 py-3">Caller</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800">
            {loading && rows.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-slate-500">
                  Loading…
                </td>
              </tr>
            )}
            {!loading && rows.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-10 text-center text-slate-500">
                  No activity in this window — try widening the time range.
                </td>
              </tr>
            )}
            {rows.map((r) => (
              <tr key={r.id} className="hover:bg-slate-900/40">
                <td className="px-4 py-3 text-slate-400" title={r.at}>
                  {relativeTime(r.at)}
                </td>
                <td className="px-4 py-3">
                  <Link
                    to={`/app/mcp-servers/${r.mcpServer.id}`}
                    className="text-brand-300 hover:underline"
                  >
                    {r.mcpServer.name}
                  </Link>
                </td>
                <td className="px-4 py-3 font-mono text-slate-200">
                  <span className="text-slate-500">{r.capabilityKind}:</span>
                  {r.capabilityName}
                </td>
                <td className="px-4 py-3">
                  <span
                    className={`rounded-full px-2 py-0.5 text-xs ${statusClasses(r.status)}`}
                    title={statusTitle(r.status)}
                  >
                    {statusLabel(r.status)}
                  </span>
                </td>
                <td className="px-4 py-3 text-slate-400">{r.latencyMs}ms</td>
                <td className="px-4 py-3 font-mono text-xs text-slate-500">
                  {truncate(JSON.stringify(r.caller ?? {}))}
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
        Invocation records are retained for 90 days. For long-term archival, plan
        for a SIEM export (follow-up).
      </p>
    </div>
  );
}

function Chip({
  active,
  onClick,
  children,
  tone,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
  tone?: string;
}) {
  const inactive = "border-slate-800 bg-slate-900/40 text-slate-400 hover:text-slate-200";
  let activeCls = "border-brand-500 bg-brand-900/40 text-brand-200";
  if (tone === "ok") activeCls = "border-emerald-600 bg-emerald-900/40 text-emerald-200";
  if (tone === "error") activeCls = "border-rose-600 bg-rose-900/40 text-rose-200";
  if (tone === "denied") activeCls = "border-amber-600 bg-amber-900/40 text-amber-200";
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded-full border px-3 py-1 text-xs ${active ? activeCls : inactive}`}
    >
      {children}
    </button>
  );
}

function SummaryCard({ summary }: { summary: ActivitySummary | null }) {
  if (!summary) {
    return (
      <div className="rounded-xl border border-slate-800 bg-slate-900/40 px-4 py-3 text-sm text-slate-500">
        Loading summary…
      </div>
    );
  }
  return (
    <div className="flex flex-wrap items-center gap-x-6 gap-y-1 rounded-xl border border-slate-800 bg-slate-900/40 px-4 py-3 text-sm">
      <span className="text-slate-400">
        <span className="font-semibold text-slate-200">
          {summary.totalInvocations}
        </span>{" "}
        total
      </span>
      <span className="text-emerald-300">{summary.byStatus.ok} ok</span>
      <span className="text-rose-300">{summary.byStatus.error} error</span>
      <span className="text-amber-300">{summary.byStatus.denied} denied</span>
      <span className="text-slate-500">
        · tool {summary.byCapabilityKind.tool} ·{" "}
        resource {summary.byCapabilityKind.resource} ·{" "}
        prompt {summary.byCapabilityKind.prompt}
      </span>
    </div>
  );
}
