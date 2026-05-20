import { Link } from "react-router-dom";
import { useEffect, useState } from "react";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { Gateway, MCPServerWithCounts } from "../api/types";
import { isOnline } from "../lib/time";

export default function OverviewPage() {
  const { activeOrgId } = useAuth();
  const [gateways, setGateways] = useState<Gateway[] | null>(null);
  const [servers, setServers] = useState<MCPServerWithCounts[] | null>(null);

  useEffect(() => {
    if (!activeOrgId) return;
    api<Gateway[]>(`/api/orgs/${activeOrgId}/gateways`).then(setGateways).catch(() => setGateways([]));
    api<MCPServerWithCounts[]>(`/api/orgs/${activeOrgId}/mcp-servers`)
      .then(setServers)
      .catch(() => setServers([]));
  }, [activeOrgId]);

  const hasGateways = (gateways?.length ?? 0) > 0;
  const onlineCount = (gateways ?? []).filter((g) => isOnline(g.lastSeenAt, g.status)).length;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Overview</h1>
        <p className="mt-1 text-sm text-slate-400">A snapshot of your MCP traffic.</p>
      </div>

      {!hasGateways ? (
        <div className="rounded-2xl border border-slate-800 bg-slate-900/40 p-10 text-center">
          <div className="mx-auto mb-4 h-12 w-12 rounded-full bg-brand-900/60 ring-1 ring-brand-700" />
          <h2 className="text-lg font-semibold">No gateways yet</h2>
          <p className="mx-auto mt-2 max-w-md text-sm text-slate-400">
            Install the Bright Guard shim on a host and it will start reporting the MCP
            servers it sees.
          </p>
          <Link
            to="/app/gateways"
            className="mt-5 inline-block rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-slate-950 hover:bg-brand-400"
          >
            Add a gateway
          </Link>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Stat label="Gateways" value={gateways?.length ?? 0} sub={`${onlineCount} online`} />
          <Stat label="MCP servers" value={servers?.length ?? 0} />
          <Stat
            label="Capabilities"
            value={(servers ?? []).reduce((acc, s) => acc + s.capabilityCount, 0)}
          />
        </div>
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
    <div className="rounded-xl border border-slate-800 bg-slate-900/40 p-5">
      <div className="text-xs uppercase tracking-wide text-slate-400">{label}</div>
      <div className="mt-1 text-3xl font-semibold">{value}</div>
      {sub && <div className="mt-1 text-xs text-slate-500">{sub}</div>}
    </div>
  );
}
