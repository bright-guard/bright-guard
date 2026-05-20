import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { OrgCallerDetail } from "../api/types";
import { relativeTime } from "../lib/time";
import PageHelp from "../components/PageHelp";
import HelpTooltip from "../components/HelpTooltip";

function statusClasses(status: string): string {
  switch (status) {
    case "ok":
      return "bg-[#006128]/10 text-[#006128]";
    case "error":
      return "bg-[#b71c1c]/10 text-[#b71c1c]";
    case "denied":
      return "bg-[#a97f13]/10 text-[#a97f13]";
    default:
      return "bg-slate-100 text-slate-600";
  }
}

export default function CallerDetailPage() {
  const { activeOrgId } = useAuth();
  const { id } = useParams<{ id: string }>();
  const [data, setData] = useState<OrgCallerDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [ackPending, setAckPending] = useState(false);

  const load = useCallback(async () => {
    if (!activeOrgId || !id) return;
    setLoading(true);
    setError(null);
    try {
      const res = await api<OrgCallerDetail>(`/api/orgs/${activeOrgId}/callers/${id}`);
      setData(res);
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
    }
  }, [activeOrgId, id]);

  useEffect(() => {
    load();
  }, [load]);

  async function acknowledge() {
    if (!activeOrgId || !id) return;
    setAckPending(true);
    try {
      await api(`/api/orgs/${activeOrgId}/callers/${id}/acknowledge`, {
        method: "POST",
      });
      await load();
    } catch (err) {
      setError(String(err));
    } finally {
      setAckPending(false);
    }
  }

  if (loading && !data) {
    return <div className="text-sm text-slate-500">Loading…</div>;
  }
  if (error) {
    return (
      <div className="rounded-md border border-rose-300 bg-rose-50 px-4 py-3 text-sm text-rose-700">
        {error}
      </div>
    );
  }
  if (!data) return null;

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <Link to="/app/callers" className="text-xs text-slate-500 hover:text-slate-900">
            ← Callers
          </Link>
          <h1 className="mt-1 flex items-center gap-2 text-2xl font-semibold">
            {data.label || "(anonymous)"}
            {data.flaggedNew && (
              <HelpTooltip term="new_caller">
                <span className="rounded-full bg-amber-100 px-2 py-0.5 text-xs uppercase tracking-wide text-amber-800">
                  new
                </span>
              </HelpTooltip>
            )}
            <PageHelp slug="activity-timeline" anchor="caller-identity" />
          </h1>
          <p
            className="mt-1 font-mono text-xs text-slate-500"
            title={data.signature}
          >
            {data.signature}
          </p>
        </div>
        {data.flaggedNew && (
          <button
            onClick={acknowledge}
            disabled={ackPending}
            className="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-sm text-slate-900 hover:border-slate-600 disabled:opacity-50"
          >
            {ackPending ? "Acknowledging…" : "Acknowledge as known"}
          </button>
        )}
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <Stat label="Invocations" value={data.invocationCount.toLocaleString()} />
        <Stat label="First seen" value={relativeTime(data.firstSeenAt)} title={data.firstSeenAt} />
        <Stat label="Last seen" value={relativeTime(data.lastSeenAt)} title={data.lastSeenAt} />
      </div>

      <section>
        <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-slate-500">
          Caller payload
        </h2>
        <pre className="overflow-x-auto rounded-xl border border-slate-200 bg-white px-4 py-3 text-xs text-slate-600">
{JSON.stringify(data.caller ?? {}, null, 2)}
        </pre>
      </section>

      <section>
        <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-slate-500">
          Top MCP servers
        </h2>
        <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
          <table className="min-w-full text-sm">
            <thead className="bg-slate-50 text-left text-xs uppercase tracking-wide text-slate-500">
              <tr>
                <th className="px-4 py-3">Server</th>
                <th className="px-4 py-3 text-right">Invocations</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-200">
              {data.topServers.length === 0 && (
                <tr>
                  <td colSpan={2} className="px-4 py-6 text-center text-slate-500">
                    No invocations recorded.
                  </td>
                </tr>
              )}
              {data.topServers.map((s) => (
                <tr key={s.mcpServerId} className="hover:bg-slate-50">
                  <td className="px-4 py-3">
                    <Link
                      to={`/app/mcp-servers/${s.mcpServerId}`}
                      className="text-brand-600 hover:underline"
                    >
                      {s.name}
                    </Link>
                  </td>
                  <td className="px-4 py-3 text-right text-slate-900">
                    {s.count.toLocaleString()}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section>
        <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-slate-500">
          Recent invocations
        </h2>
        <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
          <table className="min-w-full text-sm">
            <thead className="bg-slate-50 text-left text-xs uppercase tracking-wide text-slate-500">
              <tr>
                <th className="px-4 py-3">When</th>
                <th className="px-4 py-3">Capability</th>
                <th className="px-4 py-3">Status</th>
                <th className="px-4 py-3">Latency</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-200">
              {data.recentInvocations.length === 0 && (
                <tr>
                  <td colSpan={4} className="px-4 py-6 text-center text-slate-500">
                    No recent invocations.
                  </td>
                </tr>
              )}
              {data.recentInvocations.map((inv) => (
                <tr key={inv.id} className="hover:bg-slate-50">
                  <td className="px-4 py-3 text-slate-500" title={inv.at}>
                    {relativeTime(inv.at)}
                  </td>
                  <td className="px-4 py-3 font-mono text-slate-900">
                    <span className="text-slate-500">{inv.capabilityKind}:</span>
                    {inv.capabilityName}
                  </td>
                  <td className="px-4 py-3">
                    <span
                      className={`rounded-full px-2 py-0.5 text-xs ${statusClasses(inv.status)}`}
                    >
                      {inv.status}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-slate-500">{inv.latencyMs}ms</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

function Stat({ label, value, title }: { label: string; value: string; title?: string }) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white px-4 py-3" title={title}>
      <div className="text-xs uppercase tracking-wide text-slate-500">{label}</div>
      <div className="mt-1 text-lg font-semibold text-slate-900">{value}</div>
    </div>
  );
}
