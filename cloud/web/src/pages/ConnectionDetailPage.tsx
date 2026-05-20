import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { AuthorizeResp, MCPConnection, OAuthStatus } from "../api/types";
import { relativeTime } from "../lib/time";

const OAUTH_LABEL: Record<Exclude<OAuthStatus, "">, string> = {
  pending_authorize: "Pending authorization",
  authorized: "Authorized",
  expired_refresh: "Token expired",
  needs_reauth: "Needs re-authorization",
};

export default function ConnectionDetailPage() {
  const { activeOrgId } = useAuth();
  const { id } = useParams<{ id: string }>();
  const [conn, setConn] = useState<MCPConnection | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function reload() {
    if (!activeOrgId || !id) return;
    setLoading(true);
    try {
      const r = await api<MCPConnection>(
        `/api/orgs/${activeOrgId}/mcp-connections/${id}`,
      );
      setConn(r);
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeOrgId, id]);

  async function reauthorize() {
    if (!activeOrgId || !id) return;
    setBusy(true);
    try {
      const returnTo = encodeURIComponent(`/app/mcp-connections/${id}`);
      const auth = await api<AuthorizeResp>(
        `/api/orgs/${activeOrgId}/mcp-connections/${id}/authorize?returnTo=${returnTo}`,
      );
      window.location.href = auth.authorizeUrl;
    } catch (err) {
      setError(String(err));
      setBusy(false);
    }
  }

  if (loading) {
    return <div className="text-sm text-slate-500">Loading…</div>;
  }
  if (!conn) {
    return (
      <div className="text-sm text-rose-600">
        Connection not found.{" "}
        <Link to="/app/mcp-connections" className="underline">Back</Link>
      </div>
    );
  }

  const oauthLabel = conn.oauthStatus
    ? OAUTH_LABEL[conn.oauthStatus]
    : null;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <Link
            to="/app/mcp-connections"
            className="text-xs text-slate-500 hover:underline"
          >
            ← All connections
          </Link>
          <h1 className="mt-1 text-2xl font-semibold">{conn.name}</h1>
          <div className="mt-1 font-mono text-xs text-slate-500">{conn.endpointUrl}</div>
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Transport" value={conn.transport} />
        <Field label="Auth method" value={conn.authMethod} />
        <Field label="Status" value={conn.status} />
        <Field label="Last discovered" value={relativeTime(conn.lastDiscoveredAt)} />
      </div>

      {conn.authMethod === "oauth2_authcode" && (
        <div className="rounded-xl border border-slate-200 bg-white p-4 text-sm">
          <h2 className="font-medium">OAuth2 state</h2>
          <div className="mt-2 text-slate-500">
            {oauthLabel ?? "—"}
          </div>
          <div className="mt-3 flex gap-2">
            <button
              onClick={reauthorize}
              disabled={busy}
              className="rounded-md border border-amber-300 px-3 py-1 text-xs text-amber-800 hover:bg-amber-50 disabled:opacity-50"
            >
              {busy ? "Working…" : "Reauthorize"}
            </button>
          </div>
        </div>
      )}

      {conn.lastError && (
        <div className="rounded-md border border-rose-300 bg-rose-50 px-4 py-2 text-xs text-rose-700">
          {conn.lastError}
        </div>
      )}
      {error && (
        <div className="text-xs text-rose-600">{error}</div>
      )}
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white px-4 py-3">
      <div className="text-xs uppercase tracking-wide text-slate-500">{label}</div>
      <div className="mt-1 text-sm text-slate-900">{value || "—"}</div>
    </div>
  );
}
