import { Link } from "react-router-dom";
import { useEffect, useState } from "react";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type {
  ExposureSummary,
  Gateway,
  MCPServerWithCounts,
} from "../api/types";
import { isOnline } from "../lib/time";
import { EXPOSURE_DOT_CLASS, EXPOSURE_LABEL } from "../lib/exposure";

export default function OverviewPage() {
  const { activeOrgId } = useAuth();
  const [gateways, setGateways] = useState<Gateway[] | null>(null);
  const [servers, setServers] = useState<MCPServerWithCounts[] | null>(null);
  const [exposures, setExposures] = useState<ExposureSummary | null>(null);

  useEffect(() => {
    if (!activeOrgId) return;
    api<Gateway[]>(`/api/orgs/${activeOrgId}/gateways`).then(setGateways).catch(() => setGateways([]));
    api<MCPServerWithCounts[]>(`/api/orgs/${activeOrgId}/mcp-servers`)
      .then(setServers)
      .catch(() => setServers([]));
    api<ExposureSummary>(`/api/orgs/${activeOrgId}/exposures`)
      .then(setExposures)
      .catch(() => setExposures({ counts: [] }));
  }, [activeOrgId]);

  const hasGateways = (gateways?.length ?? 0) > 0;
  const onlineCount = (gateways ?? []).filter((g) => isOnline(g.lastSeenAt, g.status)).length;
  const totalExposure = (exposures?.counts ?? []).reduce((acc, c) => acc + c.count, 0);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Overview</h1>
        <p className="mt-1 text-sm text-slate-500">A snapshot of your MCP traffic.</p>
      </div>

      {!hasGateways ? (
        <div className="rounded-2xl border border-slate-200 bg-white p-10 text-center">
          <div className="mx-auto mb-4 h-12 w-12 rounded-full bg-brand-50 ring-1 ring-brand-300" />
          <h2 className="text-lg font-semibold">No gateways yet</h2>
          <p className="mx-auto mt-2 max-w-md text-sm text-slate-500">
            Install the Bright Guard shim on a host and it will start reporting the MCP
            servers it sees.
          </p>
          <Link
            to="/app/gateways"
            className="mt-5 inline-block rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-white hover:bg-brand-400"
          >
            Add a gateway
          </Link>
        </div>
      ) : (
        <>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <Stat label="Gateways" value={gateways?.length ?? 0} sub={`${onlineCount} online`} />
            <Stat label="MCP servers" value={servers?.length ?? 0} />
            <Stat
              label="Capabilities"
              value={(servers ?? []).reduce((acc, s) => acc + s.capabilityCount, 0)}
            />
          </div>

          {exposures && totalExposure > 0 && (
            <div className="rounded-xl border border-slate-200 bg-white p-5">
              <div className="mb-3 flex items-center justify-between">
                <h2 className="text-sm font-semibold uppercase tracking-wide text-slate-500">
                  Exposure summary
                </h2>
                <Link to="/app/mcp-servers" className="text-xs text-brand-600 hover:underline">
                  See servers
                </Link>
              </div>
              <div className="flex h-2 w-full overflow-hidden rounded-full bg-slate-200">
                {exposures.counts.map((c) =>
                  c.count > 0 ? (
                    <div
                      key={c.state}
                      title={`${EXPOSURE_LABEL[c.state]}: ${c.count}`}
                      style={{ width: `${(c.count / totalExposure) * 100}%` }}
                      className={EXPOSURE_DOT_CLASS[c.state]}
                    />
                  ) : null,
                )}
              </div>
              <div className="mt-3 flex flex-wrap gap-3 text-xs">
                {exposures.counts.map((c) => (
                  <div key={c.state} className="flex items-center gap-2 text-slate-600">
                    <span className={`inline-block h-2 w-2 rounded-full ${EXPOSURE_DOT_CLASS[c.state]}`} />
                    {EXPOSURE_LABEL[c.state]}
                    <span className="text-slate-500">{c.count}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}

function Stat({
  label,
  value,
  sub,
}: {
  label: string;
  value: number;
  sub?: string;
}) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-5">
      <div className="text-xs uppercase tracking-wide text-slate-500">{label}</div>
      <div className="mt-1 text-3xl font-semibold">{value}</div>
      {sub && <div className="mt-1 text-xs text-slate-500">{sub}</div>}
    </div>
  );
}
