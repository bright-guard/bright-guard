import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { MCPServerWithCounts } from "../api/types";
import { relativeTime } from "../lib/time";

export default function MCPServersPage() {
  const { activeOrgId } = useAuth();
  const [servers, setServers] = useState<MCPServerWithCounts[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!activeOrgId) return;
    api<MCPServerWithCounts[]>(`/api/orgs/${activeOrgId}/mcp-servers`)
      .then((s) => setServers(s ?? []))
      .finally(() => setLoading(false));
  }, [activeOrgId]);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">MCP Servers</h1>
        <p className="mt-1 text-sm text-slate-400">
          Every MCP server reported by one of your gateways.
        </p>
      </div>

      <div className="overflow-hidden rounded-xl border border-slate-800 bg-slate-900/40">
        <table className="min-w-full text-sm">
          <thead className="bg-slate-900/60 text-left text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-4 py-3">Name</th>
              <th className="px-4 py-3">Address</th>
              <th className="px-4 py-3">Transport</th>
              <th className="px-4 py-3">Capabilities</th>
              <th className="px-4 py-3">Source</th>
              <th className="px-4 py-3">Last seen</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800">
            {loading && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-slate-500">
                  Loading…
                </td>
              </tr>
            )}
            {!loading && servers.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-slate-500">
                  No MCP servers reported yet.
                </td>
              </tr>
            )}
            {servers.map((s) => (
              <tr key={s.id} className="hover:bg-slate-900/40">
                <td className="px-4 py-3">
                  <Link to={`/app/mcp-servers/${s.id}`} className="text-brand-300 hover:underline">
                    {s.name}
                  </Link>
                </td>
                <td className="px-4 py-3 text-slate-400">{s.address || "—"}</td>
                <td className="px-4 py-3 text-slate-400">{s.transport || "—"}</td>
                <td className="px-4 py-3 text-slate-400">{s.capabilityCount}</td>
                <td className="px-4 py-3 text-slate-400">
                  {s.gatewayId ? (
                    <Link to={`/app/gateways/${s.gatewayId}`} className="hover:underline">
                      <span className="text-xs text-slate-500">gateway / </span>
                      {s.gatewayName}
                    </Link>
                  ) : s.connectionId ? (
                    <Link to={`/app/mcp-connections`} className="hover:underline">
                      <span className="text-xs text-slate-500">connection / </span>
                      {s.connectionName}
                    </Link>
                  ) : (
                    "—"
                  )}
                </td>
                <td className="px-4 py-3 text-slate-400">{relativeTime(s.lastSeenAt)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
