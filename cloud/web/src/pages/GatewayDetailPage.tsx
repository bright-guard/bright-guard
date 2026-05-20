import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { GatewayDetail } from "../api/types";
import { isOnline, relativeTime } from "../lib/time";
import PageHelp from "../components/PageHelp";

export default function GatewayDetailPage() {
  const { id } = useParams();
  const { activeOrgId } = useAuth();
  const navigate = useNavigate();
  const [detail, setDetail] = useState<GatewayDetail | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!activeOrgId || !id) return;
    api<GatewayDetail>(`/api/orgs/${activeOrgId}/gateways/${id}`)
      .then(setDetail)
      .catch((err) => {
        setError(err instanceof ApiError ? `${err.status}` : String(err));
      });
  }, [activeOrgId, id]);

  async function revoke() {
    if (!activeOrgId || !id) return;
    if (!confirm("Revoke this gateway? Its credentials stop working immediately.")) return;
    await api(`/api/orgs/${activeOrgId}/gateways/${id}`, { method: "DELETE" });
    navigate("/app/gateways");
  }

  if (error) return <div className="text-rose-600">Error: {error}</div>;
  if (!detail) return <div className="text-slate-500">Loading…</div>;

  const { gateway, mcpServers } = detail;
  const online = isOnline(gateway.lastSeenAt, gateway.status);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <div className="text-xs text-slate-500">
            <Link to="/app/gateways" className="hover:underline">
              Gateways
            </Link>{" "}
            / {gateway.name}
          </div>
          <h1 className="mt-1 flex items-center gap-3 text-2xl font-semibold">
            <span
              className={`inline-block h-2.5 w-2.5 rounded-full ${
                online ? "bg-emerald-400" : "bg-slate-400"
              }`}
            />
            {gateway.name}
            <PageHelp slug="gateways/install" />
          </h1>
        </div>
        {gateway.status !== "revoked" && (
          <button
            onClick={revoke}
            className="rounded-md border border-rose-300 px-4 py-2 text-sm text-rose-700 hover:bg-rose-50"
          >
            Revoke
          </button>
        )}
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Field label="Status" value={online ? "online" : gateway.status} />
        <Field label="Last seen" value={relativeTime(gateway.lastSeenAt)} />
        <Field label="Created" value={relativeTime(gateway.createdAt)} />
      </div>
      {gateway.description && (
        <div className="rounded-xl border border-slate-200 bg-white p-4 text-sm text-slate-600">
          {gateway.description}
        </div>
      )}

      <div>
        <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-slate-500">
          MCP servers seen by this gateway
        </h2>
        {mcpServers.length === 0 ? (
          <div className="rounded-xl border border-slate-200 bg-white p-6 text-sm text-slate-500">
            No servers reported yet.
          </div>
        ) : (
          <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
            <table className="min-w-full text-sm">
              <thead className="bg-slate-50 text-left text-xs uppercase tracking-wide text-slate-500">
                <tr>
                  <th className="px-4 py-3">Name</th>
                  <th className="px-4 py-3">Transport</th>
                  <th className="px-4 py-3">Capabilities</th>
                  <th className="px-4 py-3">Last seen</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-200">
                {mcpServers.map((s) => (
                  <tr key={s.id} className="hover:bg-slate-50">
                    <td className="px-4 py-3">
                      <Link to={`/app/mcp-servers/${s.id}`} className="text-brand-600 hover:underline">
                        {s.name}
                      </Link>
                    </td>
                    <td className="px-4 py-3 text-slate-500">{s.transport || "—"}</td>
                    <td className="px-4 py-3 text-slate-500">{s.capabilityCount}</td>
                    <td className="px-4 py-3 text-slate-500">{relativeTime(s.lastSeenAt)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-4">
      <div className="text-xs uppercase tracking-wide text-slate-500">{label}</div>
      <div className="mt-1 text-sm text-slate-900">{value}</div>
    </div>
  );
}
