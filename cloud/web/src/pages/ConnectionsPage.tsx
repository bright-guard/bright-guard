import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type {
  AuthorizeResp,
  MCPConnection,
  OAuthStatus,
} from "../api/types";
import { relativeTime } from "../lib/time";
import AddConnectionWizard from "./AddConnectionWizard";
import PageHelp from "../components/PageHelp";

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

const OAUTH_CHIP: Record<Exclude<OAuthStatus, "">, { label: string; cls: string }> = {
  pending_authorize: {
    label: "Pending authorization",
    cls: "bg-[#a97f13]/10 text-[#a97f13] border-[#a97f13]/30",
  },
  authorized: {
    label: "Authorized",
    cls: "bg-[#006128]/10 text-[#006128] border-[#006128]/30",
  },
  expired_refresh: {
    label: "Token expired",
    cls: "bg-[#a97f13]/10 text-[#a97f13] border-[#a97f13]/30",
  },
  needs_reauth: {
    label: "Needs re-auth",
    cls: "bg-rose-50 text-rose-700 border-rose-300",
  },
};

export default function ConnectionsPage() {
  const { activeOrgId } = useAuth();
  const [conns, setConns] = useState<MCPConnection[]>([]);
  const [loading, setLoading] = useState(true);
  const [showAdd, setShowAdd] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const [searchParams, setSearchParams] = useSearchParams();

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

  // OAuth callback redirects land here with ?connection=...&status=ok|error.
  // Surface a one-shot toast and strip the query so refreshes don't re-fire it.
  useEffect(() => {
    const status = searchParams.get("status");
    const reason = searchParams.get("reason");
    if (!status) return;
    if (status === "ok") {
      setToast("Connection authorized.");
    } else {
      setToast(`OAuth authorization failed${reason ? `: ${reason}` : ""}.`);
    }
    const next = new URLSearchParams(searchParams);
    next.delete("status");
    next.delete("reason");
    next.delete("connection");
    setSearchParams(next, { replace: true });
    const id = window.setTimeout(() => setToast(null), 4000);
    return () => window.clearTimeout(id);
  }, [searchParams, setSearchParams]);

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

  async function reauthorize(id: string) {
    if (!activeOrgId) return;
    setBusy(id);
    try {
      const returnTo = encodeURIComponent("/app/mcp-connections");
      const auth = await api<AuthorizeResp>(
        `/api/orgs/${activeOrgId}/mcp-connections/${id}/authorize?returnTo=${returnTo}`,
      );
      window.location.href = auth.authorizeUrl;
    } catch (err) {
      console.error("authorize failed", err);
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
      {toast && (
        <div className="rounded-md border border-slate-200 bg-white px-4 py-2 text-sm text-slate-900 shadow">
          {toast}
        </div>
      )}
      <div className="flex items-center justify-between">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-2xl font-semibold">MCP Connections</h1>
            <PageHelp slug="connections/adding-an-mcp-connection" />
          </div>
          <p className="mt-1 text-sm text-slate-500">
            Direct connections to remote MCP servers — no gateway required.
          </p>
        </div>
        <button
          onClick={() => setShowAdd(true)}
          className="rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-white hover:bg-brand-400"
        >
          Add connection
        </button>
      </div>

      <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
        <table className="min-w-full text-sm">
          <thead className="bg-slate-50 text-left text-xs uppercase tracking-wide text-slate-500">
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
          <tbody className="divide-y divide-slate-200">
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
            {conns.map((c) => {
              const oauthChip = c.oauthStatus ? OAUTH_CHIP[c.oauthStatus] : null;
              return (
              <tr key={c.id} className="hover:bg-slate-50">
                <td className="px-4 py-3">
                  <span
                    className={`inline-block h-2.5 w-2.5 rounded-full ${STATUS_STYLES[c.status]}`}
                    title={STATUS_LABEL[c.status]}
                  />
                </td>
                <td className="px-4 py-3 font-medium text-slate-900">
                  <Link to={`/app/mcp-connections/${c.id}`} className="hover:underline">
                    {c.name}
                  </Link>
                </td>
                <td className="px-4 py-3 font-mono text-xs text-slate-500">{c.endpointUrl}</td>
                <td className="px-4 py-3 text-slate-500">{c.transport}</td>
                <td className="px-4 py-3 text-slate-500">
                  <div className="flex flex-col gap-1">
                    <span>{AUTH_LABEL[c.authMethod]}</span>
                    {oauthChip && (
                      <span className={`inline-block w-fit rounded-full border px-2 py-0.5 text-[10px] ${oauthChip.cls}`}>
                        {oauthChip.label}
                      </span>
                    )}
                  </div>
                </td>
                <td className="px-4 py-3 text-slate-500">
                  {relativeTime(c.lastDiscoveredAt)}
                  {c.lastError && (
                    <div className="mt-1 text-xs text-rose-600" title={c.lastError}>
                      {c.lastError.slice(0, 80)}
                    </div>
                  )}
                </td>
                <td className="px-4 py-3 text-right">
                  {c.authMethod === "oauth2_authcode" &&
                    (c.oauthStatus === "needs_reauth" ||
                      c.oauthStatus === "expired_refresh" ||
                      c.oauthStatus === "pending_authorize") ? (
                    <button
                      onClick={() => reauthorize(c.id)}
                      disabled={busy === c.id}
                      className="rounded-md border border-amber-300 px-3 py-1 text-xs text-amber-800 hover:bg-amber-50 disabled:opacity-50"
                    >
                      {busy === c.id ? "Working…" : "Reauthorize"}
                    </button>
                  ) : (
                    <button
                      onClick={() => discover(c.id)}
                      disabled={busy === c.id}
                      className="rounded-md border border-slate-300 px-3 py-1 text-xs hover:bg-slate-100 disabled:opacity-50"
                    >
                      {busy === c.id ? "Working…" : "Discover now"}
                    </button>
                  )}
                  <button
                    onClick={() => remove(c.id)}
                    disabled={busy === c.id}
                    className="ml-2 rounded-md border border-rose-300 px-3 py-1 text-xs text-rose-700 hover:bg-rose-50 disabled:opacity-50"
                  >
                    Delete
                  </button>
                </td>
              </tr>
              );
            })}
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
