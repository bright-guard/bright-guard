import { useEffect, useState } from "react";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { MCPConnection } from "../api/types";
import { relativeTime } from "../lib/time";
import AddConnectionWizard from "./AddConnectionWizard";

const STATUS_STYLES: Record<MCPConnection["status"], string> = {
  healthy: "bg-emerald-400",
  pending: "bg-slate-500",
  error: "bg-rose-500",
  unauthorized: "bg-amber-500",
};

const STATUS_LABEL: Record<MCPConnection["status"], string> = {
  healthy: "healthy",
  pending: "pending",
  error: "error",
  unauthorized: "auth failed",
};

const AUTH_LABEL: Record<MCPConnection["authMethod"], string> = {
  api_key_header: "API key",
  bearer: "Bearer",
  basic: "Basic",
  oauth2_authcode: "OAuth2",
};

export default function ConnectionsPage() {
  const { activeOrgId } = useAuth();
  const [conns, setConns] = useState<MCPConnection[]>([]);
  const [loading, setLoading] = useState(true);
  const [showAdd, setShowAdd] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);

  async function reload() {
    if (!activeOrgId) return;
    setLoading(true);
    try {
      const list = await api<MCPConnection[]>(
        `/api/orgs/${activeOrgId}/mcp-connections`,
      );
      setConns(list ?? []);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeOrgId]);

  async function discover(id: string) {
    if (!activeOrgId) return;
    setBusy(id);
    try {
      await api(`/api/orgs/${activeOrgId}/mcp-connections/${id}/discover`, {
        method: "POST",
      });
      await reload();
    } catch (err) {
      console.error("discover failed", err);
    } finally {
      setBusy(null);
    }
  }

  async function remove(id: string) {
    if (!activeOrgId) return;
    if (!confirm("Delete this connection? Its discovered server will be removed.")) return;
    setBusy(id);
    try {
      await api(`/api/orgs/${activeOrgId}/mcp-connections/${id}`, {
        method: "DELETE",
      });
      await reload();
    } catch (err) {
      console.error("delete failed", err);
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">MCP Connections</h1>
          <p className="mt-1 text-sm text-slate-400">
            Direct connections to remote MCP servers — no gateway required.
          </p>
        </div>
        <button
          onClick={() => setShowAdd(true)}
          className="rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-slate-950 hover:bg-brand-400"
        >
          Add connection
        </button>
      </div>

      <div className="overflow-hidden rounded-xl border border-slate-800 bg-slate-900/40">
        <table className="min-w-full text-sm">
          <thead className="bg-slate-900/60 text-left text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="w-12 px-4 py-3"></th>
              <th className="px-4 py-3">Name</th>
              <th className="px-4 py-3">Endpoint</th>
              <th className="px-4 py-3">Transport</th>
              <th className="px-4 py-3">Auth</th>
              <th className="px-4 py-3">Last discovered</th>
              <th className="px-4 py-3"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800">
            {loading && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-slate-500">
                  Loading…
                </td>
              </tr>
            )}
            {!loading && conns.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-slate-500">
                  No connections yet. Add one to discover a remote MCP server.
                </td>
              </tr>
            )}
            {conns.map((c) => (
              <tr key={c.id} className="hover:bg-slate-900/40">
                <td className="px-4 py-3">
                  <span
                    className={`inline-block h-2.5 w-2.5 rounded-full ${STATUS_STYLES[c.status]}`}
                    title={STATUS_LABEL[c.status]}
                  />
                </td>
                <td className="px-4 py-3 font-medium text-slate-200">{c.name}</td>
                <td className="px-4 py-3 font-mono text-xs text-slate-400">{c.endpointUrl}</td>
                <td className="px-4 py-3 text-slate-400">{c.transport}</td>
                <td className="px-4 py-3 text-slate-400">{AUTH_LABEL[c.authMethod]}</td>
                <td className="px-4 py-3 text-slate-400">
                  {relativeTime(c.lastDiscoveredAt)}
                  {c.lastError && (
                    <div className="mt-1 text-xs text-rose-400" title={c.lastError}>
                      {c.lastError.slice(0, 80)}
                    </div>
                  )}
                </td>
                <td className="px-4 py-3 text-right">
                  <button
                    onClick={() => discover(c.id)}
                    disabled={busy === c.id}
                    className="rounded-md border border-slate-700 px-3 py-1 text-xs hover:bg-slate-800 disabled:opacity-50"
                  >
                    {busy === c.id ? "Working…" : "Discover now"}
                  </button>
                  <button
                    onClick={() => remove(c.id)}
                    disabled={busy === c.id}
                    className="ml-2 rounded-md border border-rose-900/60 px-3 py-1 text-xs text-rose-300 hover:bg-rose-950/40 disabled:opacity-50"
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {showAdd && activeOrgId && (
        <AddConnectionWizard
          orgId={activeOrgId}
          onClose={() => setShowAdd(false)}
          onDone={async () => {
            setShowAdd(false);
            await reload();
          }}
        />
      )}
    </div>
  );
}

export { ApiError };
